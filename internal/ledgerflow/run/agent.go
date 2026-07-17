package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
)

// agent.go wires the §8 judgment agent into the ledger flow (SPEC §5, §8, §11
// Phase 7a). It builds the swappable agent client for the requested mode, asks it
// to review an unresolved event, records the recommendation, and persists the
// FROZEN trace of every consultation. Agent output is never bound or posted.
//
// The agent NEVER emits raw debits/credits (SPEC §3, §8); it returns only an
// entry-type name and paise params as a review recommendation. The recommendation
// is captured in the trace and is not bound, posted, or scored as produced output.

// agentProvider lazily builds the §8 classify client. It validates the requested
// mode up front (so a typo fails fast) but defers loading the recorded-response
// fixture until the FIRST rule miss actually needs the agent. That laziness is
// what lets a CLEAN period (no misses) run in --agent replay WITHOUT a recorded
// fixture present — the agent is simply never consulted — while a period that DOES
// have misses still fails loudly if its fixture is missing or malformed (SPEC §12).
type agentProvider struct {
	mode    agentclient.Mode
	root    string
	world   string
	period  string
	liveURL string

	client agentclient.Client // built on first use
	built  bool

	invClient agentclient.InvestigateClient // §8 investigate client, built on first use
	invBuilt  bool
}

// newAgent validates opts.Agent and returns the provider and resolved mode. It
// does NO IO (no fixture load) — the fixture is loaded lazily on first miss. An
// unknown mode is an error so a typo never silently runs agent-off; a live mode
// with no URL is rejected up front.
func newAgent(root, world, period string, opts agentOptions) (*agentProvider, agentclient.Mode, error) {
	switch opts.Agent {
	case "", agentclient.ModeOff:
		return &agentProvider{mode: agentclient.ModeOff}, agentclient.ModeOff, nil
	case agentclient.ModeReplay:
		return &agentProvider{mode: agentclient.ModeReplay, root: root, world: world, period: period}, agentclient.ModeReplay, nil
	case agentclient.ModeLive:
		if opts.LiveBaseURL == "" {
			return nil, agentclient.ModeLive, fmt.Errorf("closer: live agent mode requires a Flue base URL")
		}
		return &agentProvider{mode: agentclient.ModeLive, root: root, world: world, period: period, liveURL: opts.LiveBaseURL}, agentclient.ModeLive, nil
	default:
		return nil, opts.Agent, fmt.Errorf("closer: unknown agent mode %q (want off|replay|live)", opts.Agent)
	}
}

// enabled reports whether the agent should be consulted at all (any mode other
// than off).
func (p *agentProvider) enabled() bool { return p.mode != agentclient.ModeOff && p.mode != "" }

// client lazily builds and caches the concrete client for the provider's mode. In
// replay mode this loads the committed recorded-response fixture (a missing or
// malformed fixture is a hard error — a replay run with real misses must have
// something to replay). In live mode it builds the HTTP client against the
// configured Flue URL.
func (p *agentProvider) clientFor() (agentclient.Client, error) {
	if p.built {
		return p.client, nil
	}
	var (
		c   agentclient.Client
		err error
	)
	switch p.mode {
	case agentclient.ModeReplay:
		c, err = agentclient.NewReplayClientFromPath(agentclient.RecordedPath(p.root, p.world, p.period))
		if err != nil {
			return nil, fmt.Errorf("closer: build replay agent: %w", err)
		}
	case agentclient.ModeLive:
		c = agentclient.NewLiveClient(p.liveURL, p.world, p.period, "", nil)
	default:
		return nil, fmt.Errorf("closer: agent mode %q has no client", p.mode)
	}
	p.client = c
	p.built = true
	return c, nil
}

// investigateClientFor lazily builds and caches the concrete §8 INVESTIGATE client
// for the provider's mode (Phase 8), parallel to clientFor. In replay mode it loads
// the committed investigate.recorded.json (a missing/malformed fixture is a hard
// error — a replay run with real breaks must have recorded resolutions to replay);
// in live mode it builds the HTTP client against the configured Flue URL. It is
// only reached when reconcile actually found a break, so a fully-reconciling period
// never requires an investigate fixture.
func (p *agentProvider) investigateClientFor() (agentclient.InvestigateClient, error) {
	if p.invBuilt {
		return p.invClient, nil
	}
	var (
		c   agentclient.InvestigateClient
		err error
	)
	switch p.mode {
	case agentclient.ModeReplay:
		c, err = agentclient.NewReplayInvestigateClientFromPath(agentclient.InvestigateRecordedPath(p.root, p.world, p.period))
		if err != nil {
			return nil, fmt.Errorf("closer: build replay investigate agent: %w", err)
		}
	case agentclient.ModeLive:
		c = agentclient.NewLiveInvestigateClient(p.liveURL, p.world, p.period, "", nil)
	default:
		return nil, fmt.Errorf("closer: agent mode %q has no investigate client", p.mode)
	}
	p.invClient = c
	p.invBuilt = true
	return c, nil
}

// agentOptions is the subset of Options newAgent needs. It is the same fields as
// Options; kept as its own type only so newAgent's signature is self-describing.
type agentOptions = Options

// resolver returns the shared-spine resolveMiss strategy for the SYNCHRONOUS inline
// judgment agent. Recovery calls this only when it cannot produce a safe candidate.
// The agent recommendation is review-only and never marks the event handled.
// It always returns the FROZEN trace of a consultation so the loop records it. The
// recorded fixture is loaded lazily on the first miss, so a clean period needs none.
func (p *agentProvider) resolver() resolveMiss {
	return func(ev ingest.NormalizedEvent, context json.RawMessage) (*posting.Classification, string, bool, *agentclient.Trace, error) {
		if p == nil || !p.enabled() {
			return nil, "", false, nil, nil // agent off: skip with the rule reason.
		}
		agent, err := p.clientFor()
		if err != nil {
			return nil, "", false, nil, err
		}
		out, trace, err := agent.Classify(agentclient.SummarizeEvent(ev), context)
		if err != nil {
			return nil, "", false, nil, fmt.Errorf("closer: agent classify event %s: %w", ev.ID, err)
		}
		if !out.Classifiable() {
			return nil, agentEscalationReason(out), false, &trace, nil
		}
		return nil, agentReviewReason(out), false, &trace, nil
	}
}

func agentReviewReason(out agentclient.ClassifyResult) string {
	if out.Rationale != "" {
		return "agent review: " + out.Rationale
	}
	return "agent review: recommendation recorded; no automatic posting"
}

// agentEscalationReason renders the reason an escalated event ends up skipped,
// attributing it to the agent so the operator can tell a rule miss from an agent
// escalation.
func agentEscalationReason(out agentclient.ClassifyResult) string {
	if out.Reason != "" {
		return "agent escalated: " + out.Reason
	}
	return "agent escalated (no reason given)"
}

// skipReason composes the human-readable reason for a skipped event. An agent
// reason is most specific; when the agent is off, recovery explains why the
// event was not safe to post, followed by the original posting-rule miss.
func skipReason(recoveryReason, ruleReason, agentReason string) string {
	if agentReason != "" {
		return agentReason
	}
	if recoveryReason != "" {
		return recoveryReason
	}
	return ruleReason
}

// writeTraces persists the FROZEN agent traces to runs/<world>-<period>/trace.json
// as a stable JSON array (SPEC §9, §10, §13), so `show trace runs/<...>` can print
// the agent trajectory. The file is written with the same canonical encoding the
// rest of the project uses (2-space indent, no HTML escaping, trailing newline), so
// it is byte-stable across two identical replay runs.
func writeTraces(root, world, period string, traces []agentclient.Trace) (string, error) {
	dir := filepath.Join(root, "runs", world+"-"+period)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("closer: create run dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "trace.json")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(traces); err != nil {
		return "", fmt.Errorf("closer: marshal traces: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("closer: write traces %s: %w", path, err)
	}
	return path, nil
}
