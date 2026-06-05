package closer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/classify"
	"github.com/razorpay/close-agent/internal/config"
	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/reconcile"
)

// investigate.go wires the §8 INVESTIGATE agent into the close (SPEC §5, §7, §8,
// Phase 8), parallel to agent.go for classify. After reconcile lists breaks, the
// orchestrator hands each break (plus the unbooked candidate events) to the
// swappable investigate client. A resolution's postings are bound+posted through
// the ledger (balance-or-reject) exactly like a classification — the agent emits
// only {entry_type, params} and the orchestrator owns the IK/TxID/Ts and the
// event attribution — then the ledger is RE-RECONCILED so a genuinely-fixed break
// clears. A break the agent ESCALATES (or any break when no posting can fix it) is
// left listed and recorded as an Escalation; the orchestrator never guesses.
//
// The investigate agent NEVER reads internal/truth: the resolution is derived from
// the snapshotted agent-input fixtures (refunds.json / orders.json). The
// truth-isolation guard confirms closer (and agentclient) never import truth.

// investigateBreaks consults the §8 investigate agent for each break on res.Breaks,
// applies any resolutions to lg, re-reconciles, and records the outcome on res. The
// candidates are the unbooked events (the "settled-but-not-booked" suspects) the
// agent inspects. reconInput rebuilds the reconcile input against the (now-updated)
// ledger so the re-reconcile reflects the posted resolutions.
//
// It returns an error only for an agent infrastructure failure (a missing/malformed
// recorded fixture, a live transport error, or a resolution that fails to bind/post)
// — never for a break the agent declines, which is a normal escalation.
func investigateBreaks(
	p *agentProvider,
	tmpls ledger.Templates,
	lg *ledger.Ledger,
	eventByID map[string]ingest.NormalizedEvent,
	candidates []agentclient.EventSummary,
	reconInput func() reconcile.Input,
	res *Result,
) error {
	client, err := p.investigateClientFor()
	if err != nil {
		return err
	}

	for _, b := range res.Breaks {
		bs := breakSummaryFrom(b)
		out, trace, err := client.Investigate(bs, candidates)
		if err != nil {
			return fmt.Errorf("closer: investigate break %s: %w", bs.Key, err)
		}
		res.InvestigateTraces = append(res.InvestigateTraces, trace)

		if !out.Resolvable() {
			res.Escalations = append(res.Escalations, Escalation{
				BreakKey: bs.Key, Kind: b.Kind, Reason: investigateEscalationReason(out),
			})
			continue
		}

		for _, post := range out.Resolution {
			if err := applyPosting(tmpls, lg, eventByID, post, res); err != nil {
				return fmt.Errorf("closer: apply resolution for break %s: %w", bs.Key, err)
			}
		}
		res.InvestigateDone++
	}

	// Re-reconcile against the now-updated ledger: a resolution that genuinely fixed
	// the books clears its break; an escalated or unfixed break reappears here and
	// stays listed. res.Breaks becomes the FINAL unresolved set.
	res.Breaks = reconcile.Reconcile(reconInput())
	return nil
}

// applyPosting binds and posts one resolution posting through the ledger, deriving
// the idempotency key and TxID from the posting's entry type + source event id
// (the SAME scheme the rule engine / classify agent use, so the entry lines up with
// truth), stamping the source event's timestamp, and appending the produced entry
// to res.Produced so the scorer counts it. The agent supplies only entry_type +
// params; the bookkeeping is the orchestrator's (SPEC §8).
func applyPosting(
	tmpls ledger.Templates,
	lg *ledger.Ledger,
	eventByID map[string]ingest.NormalizedEvent,
	post agentclient.Posting,
	res *Result,
) error {
	ik, txID, err := entryRefsForType(post.EntryType, post.EventID)
	if err != nil {
		return err
	}
	entry, err := ledger.Bind(tmpls, post.EntryType, ik, post.Params)
	if err != nil {
		return fmt.Errorf("bind %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	if ev, ok := eventByID[post.EventID]; ok {
		entry.Ts = ev.TS
	}
	entry.TxID = txID
	posted, err := lg.Post(entry)
	if err != nil {
		return fmt.Errorf("post %s for event %s: %w", post.EntryType, post.EventID, err)
	}
	res.Produced = append(res.Produced, producedFrom(post.EventID, posted))
	return nil
}

// breakSummaryFrom projects a reconcile.Break onto the agentclient.BreakSummary the
// §8 investigate agent sees, stamping the stable break key.
func breakSummaryFrom(b reconcile.Break) agentclient.BreakSummary {
	return agentclient.BreakSummary{
		Key:          breakKey(b),
		Check:        int(b.Check),
		Kind:         b.Kind,
		SettlementID: b.SettlementID,
		Expected:     b.Expected,
		Actual:       b.Actual,
		Candidates:   b.CandidateEventIDs,
		Detail:       b.Detail,
	}
}

// breakKey is the stable identifier the recorded investigation fixture is keyed by:
// the check number, the kind, and the settlement id. A period-wide break (the
// receivable residual) has an empty settlement id, which is fine — there is at most
// one such break per period in v1.
func breakKey(b reconcile.Break) string {
	return fmt.Sprintf("check%d:%s:%s", b.Check, b.Kind, b.SettlementID)
}

// entryRefsForType derives the idempotency key and external tx-id string for an
// investigate-posted entry from its entry type and source event id, mirroring the
// rule engine's per-type scheme (internal/classify) so an investigate-booked entry
// matches truth's IK/TxID exactly. In v1 the investigate agent books refund_reversal
// (the settled-but-not-booked refund); the other types are mapped for completeness.
func entryRefsForType(entryType, eventID string) (ik, txID string, err error) {
	switch entryType {
	case "dtc_sale":
		return "sale:" + eventID, eventID, nil
	case "refund_reversal":
		return "refund:" + eventID, eventID, nil
	case "chargeback_loss":
		return "dispute:" + eventID, eventID, nil
	case "razorpay_settlement":
		return "settle:" + eventID, eventID, nil
	default:
		return "", "", fmt.Errorf("no IK/TxID scheme for entry type %q", entryType)
	}
}

// summarizeEvents projects normalized events onto the §8 EventSummary candidates
// the investigate agent inspects, reusing agentclient.SummarizeEvent.
func summarizeEvents(evs []ingest.NormalizedEvent) []agentclient.EventSummary {
	out := make([]agentclient.EventSummary, 0, len(evs))
	for _, ev := range evs {
		out = append(out, agentclient.SummarizeEvent(ev))
	}
	return out
}

// investigateEscalationReason renders the reason an escalated break ends up listed,
// attributing it to the agent.
func investigateEscalationReason(out agentclient.InvestigateResult) string {
	if out.Reason != "" {
		return "agent escalated: " + out.Reason
	}
	return "agent escalated (no reason given)"
}

// writeInvestigateTraces persists the FROZEN investigate traces to
// runs/<world>-<period>/investigate-trace.json as a stable JSON array (SPEC §9,
// §10, §13), so `show trace <path>` can print the investigate trajectory. Same
// canonical encoding as the classify traces, so it is byte-stable across two
// identical replay runs.
func writeInvestigateTraces(root, world, period string, traces []agentclient.InvestigateTrace) (string, error) {
	dir := filepath.Join(root, "runs", world+"-"+period)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("closer: create run dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "investigate-trace.json")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(traces); err != nil {
		return "", fmt.Errorf("closer: marshal investigate traces: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("closer: write investigate traces %s: %w", path, err)
	}
	return path, nil
}

// GenerateInvestigateRecorded builds the recorded-investigation fixture for
// (world, period) under root, parallel to agentclient.GenerateRecorded for
// classify (SPEC §2, §8, §12). It is the reproducible, reviewable generator behind
// the committed investigate.recorded.json: running it on the committed break period
// reproduces the committed file byte-for-byte (a test asserts it).
//
// It runs the close pipeline up to reconcile with the REPLAY classify agent (so the
// breaks match exactly what a `close --agent replay` run sees BEFORE investigate),
// then for each break derives a resolution from the snapshotted agent-input
// fixtures — NEVER truth. For a receivable-residual break (the "settled-but-not-
// booked" case) it traces the residual to the unbooked refund(s) among the skipped
// events, recovers each refund's gst_rate from its parent order (orders.json), and
// reconstructs the SAME refund_reversal the rule engine would have produced had the
// rate been present (it literally re-runs classify on the rate-restored event, so
// the booked entry equals truth to the paise). A break no posting can resolve is
// recorded as a deterministic escalation.
func GenerateInvestigateRecorded(root, world, period string) (agentclient.RecordedInvestigateFile, error) {
	pb, err := config.DefaultPlaybook()
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, fmt.Errorf("closer: load playbook: %w", err)
	}
	tmpls := ledger.NewPlaybookTemplates(pb)
	lg := ledger.New(ledger.NewPlaybookChart(pb))

	raw, events, err := ingest.IngestAndNormalize(root, world, period)
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}

	// Replay classify so recovered events book exactly as the runtime does; on this
	// break period the only miss is the rate-stripped refund, which classify escalates
	// (it recovers payments, not refunds) so it stays unbooked for investigate.
	agent, _, err := newAgent(root, world, period, Options{Agent: agentclient.ModeReplay})
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}

	scratch := &Result{Ledger: lg}
	var skipped []ingest.NormalizedEvent
	for _, ev := range events {
		c, ok, _ := classify.Classify(ev)
		if !ok {
			ac, _, handled, err := classifyWithAgent(agent, ev, scratch)
			if err != nil {
				return agentclient.RecordedInvestigateFile{}, err
			}
			if !handled {
				skipped = append(skipped, ev)
				continue
			}
			c = ac
		}
		entry, err := ledger.Bind(tmpls, c.EntryType, c.IK, c.Params)
		if err != nil {
			return agentclient.RecordedInvestigateFile{}, fmt.Errorf("closer: bind %s for event %s: %w", c.EntryType, ev.ID, err)
		}
		entry.Ts = c.Ts
		entry.TxID = c.TxID
		if _, err := lg.Post(entry); err != nil {
			return agentclient.RecordedInvestigateFile{}, fmt.Errorf("closer: post %s for event %s: %w", c.EntryType, ev.ID, err)
		}
	}

	periodEnd, err := nextMonthFirst(period)
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}
	breaks := reconcile.Reconcile(reconcile.Input{
		Settlements:       raw.Settlements,
		Payments:          raw.Payments,
		Refunds:           raw.Refunds,
		BankFeed:          raw.BankFeed,
		ReceivableBalance: lg.AccountBalance(receivableAccount).Balance,
		PeriodEnd:         periodEnd,
		DateToleranceDays: settlementDateToleranceDays,
	})

	// Recovery sources (agent-input, never truth): the order gst_rate per order id,
	// and the payment id -> order id map so a refund (which links only to its
	// payment) can reach its order's rate.
	rates, err := agentclient.OrderGSTRates(root, world, period)
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}
	payOrder := make(map[string]string, len(raw.Payments))
	for _, p := range raw.Payments {
		payOrder[p.ID] = p.OrderID
	}

	f := agentclient.RecordedInvestigateFile{
		SchemaVersion: agentclient.InvestigateRecordedSchemaVersion,
		World:         world,
		Period:        period,
		Resolutions:   make([]agentclient.RecordedResolution, 0, len(breaks)),
	}
	for _, b := range breaks {
		f.Resolutions = append(f.Resolutions, recoverBreak(b, skipped, payOrder, rates))
	}
	return f, nil
}

// orderFetchTool is the read-only tool recorded in an investigation's tools_used:
// the agent fetched the order to recover an unbooked refund's gst_rate (SPEC §8).
const orderFetchTool = "orders.fetch"

// recoverBreak derives the recorded resolution for one break from snapshotted
// agent-input (never truth). For a receivable-residual break it traces the residual
// to the unbooked refund(s) among the skipped events and reconstructs each
// refund_reversal by recovering the rate from the parent order and re-running the
// rule engine on the rate-restored event (so the booking equals truth). Any other
// break — or a residual it cannot explain — is a deterministic escalation, since no
// ledger posting resolves a check #1 (cash) or check #2 (batch-data) break.
func recoverBreak(
	b reconcile.Break,
	skipped []ingest.NormalizedEvent,
	payOrder map[string]string,
	rates map[string]string,
) agentclient.RecordedResolution {
	key := breakKey(b)
	if b.Kind != reconcile.KindReceivableResidual {
		return agentclient.RecordedResolution{
			BreakKey: key,
			Escalate: true,
			Reason: fmt.Sprintf("a %s break cannot be resolved by a posting (it is a %s inconsistency); needs a data correction or human review",
				b.Kind, checkConcern(b.Check)),
			ToolsUsed: []string{},
		}
	}

	postings := make([]agentclient.RecordedPosting, 0, 1)
	for _, ev := range skipped {
		if ev.Type != ingest.EventRefund {
			continue
		}
		orderID, ok := payOrder[ev.Links.PaymentID]
		if !ok {
			continue
		}
		rate, ok := rates[orderID]
		if !ok || rate == "" {
			continue
		}
		// Reconstruct the refund_reversal the rule engine would have produced with the
		// recovered rate: copy the event with the rate restored and re-run classify, so
		// the params (net/gst) are byte-identical to truth.
		restored := ev
		notes := ingest.Notes{GSTRate: rate}
		if ev.Notes != nil {
			notes = *ev.Notes
			notes.GSTRate = rate
		}
		restored.Notes = &notes
		c, ok, _ := classify.Classify(restored)
		if !ok {
			continue
		}
		params := make(map[string]int64, len(c.Params))
		for k, v := range c.Params {
			params[k] = v.Paise()
		}
		postings = append(postings, agentclient.RecordedPosting{
			EventID:   ev.ID,
			EntryType: c.EntryType,
			Params:    params,
		})
	}

	if len(postings) == 0 {
		return agentclient.RecordedResolution{
			BreakKey:  key,
			Escalate:  true,
			Reason:    "could not recover an unbooked refund explaining the receivable residual",
			ToolsUsed: []string{orderFetchTool},
		}
	}
	residual := b.Actual.Sub(b.Expected)
	return agentclient.RecordedResolution{
		BreakKey:   key,
		Resolution: postings,
		Rationale: fmt.Sprintf("settlement-receivable residual %s traced to %d unbooked refund(s); fetched each refund's order, recovered its gst_rate, and booked the refund_reversal so the receivable clears",
			residual, len(postings)),
		ToolsUsed: []string{orderFetchTool},
	}
}

// checkConcern names what a check is about, for an escalation reason.
func checkConcern(check reconcile.CheckNum) string {
	switch check {
	case reconcile.CheckSettlementBank:
		return "settlement-vs-bank cash"
	case reconcile.CheckBatchSum:
		return "settlement batch-data"
	default:
		return "reconciliation"
	}
}
