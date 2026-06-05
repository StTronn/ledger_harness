package closer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/ingest"
)

// agent.go wires the §8 classify agent into the close loop (SPEC §5, §8, §11
// Phase 7a). It builds the swappable agent client for the requested mode, asks it
// to classify a rule-missed event, turns the agent's {entry_type, params} into the
// same classify.Classification the rule engine produces (so the orchestrator binds
// and posts it identically), and records the FROZEN trace of every consultation.
//
// The agent NEVER emits raw debits/credits (SPEC §3, §8); it returns only an
// entry-type name and paise params. The IK and TxID — which the scorer matches on
// and which must equal truth's scheme — are derived HERE from the event using the
// SAME scheme the rule engine uses (classify's "sale:"+id IK, payment id as TxID),
// not trusted from the agent. So an agent-classified entry is byte-identical to
// what the rule would have produced, and replaying recovered responses raises the
// score to exactly truth.

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

// agentOptions is the subset of Options newAgent needs. It is the same fields as
// Options; kept as its own type only so newAgent's signature is self-describing.
type agentOptions = Options

// classifyWithAgent consults the agent for a rule-missed event and, if the agent
// classified it, returns the bindable Classification (handled=true). When the
// agent is off, or the agent escalated ({unclassifiable}), it returns handled=false
// and the reason to fold into the Skip. Every consultation (including an
// escalation) appends a FROZEN trace to res.Traces and increments res.AgentDone on
// a successful classification.
//
// It returns an error only for an agent infrastructure failure (a missing/malformed
// recorded fixture, a live transport error) — never for an event the agent
// declines, which is a normal escalation. The recorded fixture is loaded lazily on
// THIS first miss, so a clean period never requires one.
func classifyWithAgent(p *agentProvider, ev ingest.NormalizedEvent, res *Result) (*classify.Classification, string, bool, error) {
	if p == nil || !p.enabled() {
		return nil, "", false, nil // agent off: caller flags-and-skips with the rule reason.
	}
	agent, err := p.clientFor()
	if err != nil {
		return nil, "", false, err
	}

	summary := agentclient.SummarizeEvent(ev)
	out, trace, err := agent.Classify(summary)
	if err != nil {
		return nil, "", false, fmt.Errorf("closer: agent classify event %s: %w", ev.ID, err)
	}
	res.Traces = append(res.Traces, trace)

	if !out.Classifiable() {
		return nil, agentEscalationReason(out), false, nil
	}

	c, err := classificationFromAgent(ev, out)
	if err != nil {
		return nil, "", false, fmt.Errorf("closer: bind agent decision for event %s: %w", ev.ID, err)
	}
	res.AgentDone++
	return c, "", true, nil
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

// skipReason composes the human-readable reason for a skipped event. When the
// agent supplied an escalation reason it is used (it is the more specific, agent-
// level reason); otherwise the rule-miss reason stands (the agent-off baseline).
func skipReason(ruleReason, agentReason string) string {
	if agentReason != "" {
		return agentReason
	}
	return ruleReason
}

// classificationFromAgent turns the agent's {entry_type, params} into the
// orchestrator's Classification, deriving the IK and TxID from the event with the
// SAME scheme the rule engine uses (so the scorer lines the entry up with truth).
// The agent supplies only the entry type and paise params; the bookkeeping (IK,
// TxID, Ts) is the orchestrator's, never the agent's — keeping the agent's surface
// to exactly {entry_type, params} (SPEC §8).
func classificationFromAgent(ev ingest.NormalizedEvent, out agentclient.ClassifyResult) (*classify.Classification, error) {
	ik, txID, err := agentEntryRefs(ev)
	if err != nil {
		return nil, err
	}
	return &classify.Classification{
		EntryType: out.EntryType,
		Params:    out.Params,
		IK:        ik,
		TxID:      txID,
		Ts:        ev.TS,
		Reason:    "agent: " + out.Rationale,
	}, nil
}

// agentEntryRefs derives the idempotency key and external tx-id string for an
// agent-classified event, mirroring the rule engine's per-type scheme
// (internal/classify) so an agent-booked entry matches truth's IK/TxID exactly. In
// the v1 hard period the agent only recovers PAYMENTS (dtc_sale), so a payment's
// scheme (IK "sale:"+id, TxID = payment id) is the one exercised; the other event
// types are mapped for completeness and future-proofing.
func agentEntryRefs(ev ingest.NormalizedEvent) (ik, txID string, err error) {
	switch ev.Type {
	case ingest.EventPayment:
		return "sale:" + ev.ID, ev.ID, nil
	case ingest.EventRefund:
		return "refund:" + ev.ID, ev.ID, nil
	case ingest.EventDispute:
		return "dispute:" + ev.ID, ev.ID, nil
	case ingest.EventSettlement:
		// A settlement's TxID is its bank UTR, which the rule engine reads from raw;
		// the agent does not recover settlements in v1, so this path is not reached.
		return "settle:" + ev.ID, ev.ID, nil
	default:
		return "", "", fmt.Errorf("no IK/TxID scheme for event type %q", ev.Type)
	}
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
