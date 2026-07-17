package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/razorpay/ledger-flow/internal/agentclient"
	"github.com/razorpay/ledger-flow/internal/gstsplit"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery/policychecks"
	"github.com/razorpay/ledger-flow/internal/reconcile"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// investigate.go wires the §8 INVESTIGATE agent into the close (SPEC §5, §7, §8,
// Phase 8), parallel to agent.go for posting. After reconcile lists breaks, the
// orchestrator hands each break (plus the unbooked candidate events) to the
// swappable investigate client. The agent's recommendation is recorded for review
// but is never bound or posted automatically. Breaks remain listed until an
// explicit deterministic or human-approved posting path handles them.
//
// The investigate agent NEVER reads internal/truth: the resolution is derived from
// the snapshotted agent-input fixtures (refunds.json / orders.json). The
// truth-isolation guard confirms closer (and agentclient) never import truth.

// investigateBreaks consults the §8 investigate agent for each break on res.Breaks
// and records the recommendation on res. The candidates are the unbooked events
// (the "settled-but-not-booked" suspects) the agent inspects. The ledger remains
// unchanged, so the breaks stay available for explicit review or approval.
//
// It returns an error only for an agent infrastructure failure (a missing/malformed
// recorded fixture or a live transport error)
// — never for a break the agent declines, which is a normal escalation.
func investigateBreaks(
	p *agentProvider,
	recoveryEngine *recovery.Engine,
	candidates []agentclient.EventSummary,
	res *Result,
) error {
	client, err := p.investigateClientFor()
	if err != nil {
		return err
	}

	for _, b := range res.Breaks {
		bs := breakSummaryFrom(b)
		var context json.RawMessage
		if recoveryEngine != nil {
			if bundle, ok := recoveryEngine.BreakContext(bs.Key); ok {
				context, err = json.Marshal(bundle)
				if err != nil {
					return fmt.Errorf("closer: marshal recovery context for break %s: %w", bs.Key, err)
				}
			}
		}
		out, trace, err := client.Investigate(bs, candidates, context)
		if err != nil {
			return fmt.Errorf("closer: investigate break %s: %w", bs.Key, err)
		}
		res.InvestigateTraces = append(res.InvestigateTraces, trace)

		res.InvestigateReviewed++
		res.Escalations = append(res.Escalations, Escalation{
			BreakKey: bs.Key, Kind: b.Kind, Reason: investigateReviewReason(out),
		})
	}

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
	return b.Key()
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

func investigateReviewReason(out agentclient.InvestigateResult) string {
	if out.Escalate {
		return investigateEscalationReason(out)
	}
	if out.Rationale != "" {
		return "agent recommendation for review: " + out.Rationale
	}
	return "agent recommendation recorded for review; no automatic posting"
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
	// Run the shared book loop with the REPLAY classify resolver so the breaks match
	// exactly what `close --agent replay` sees BEFORE investigate; on this break
	// period the only miss is the rate-stripped refund, which classify escalates (it
	// recovers payments, not refunds), so it stays unbooked for investigate.
	agent, _, err := newAgent(root, world, period, Options{Agent: agentclient.ModeReplay})
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}
	er, err := runEvents(root, world, period, agent.resolver())
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}

	periodEnd, err := nextMonthFirst(period)
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}
	breaks := reconcile.Reconcile(er.reconcileInput(periodEnd))

	// Recovery sources (agent-input, never truth): the order gst_rate per order id,
	// and the payment id -> order id map so a refund (which links only to its
	// payment) can reach its order's rate.
	rates, err := agentclient.OrderGSTRates(root, world, period)
	if err != nil {
		return agentclient.RecordedInvestigateFile{}, err
	}
	payOrder := make(map[string]string, len(er.raw.Payments))
	for _, p := range er.raw.Payments {
		payOrder[p.ID] = p.OrderID
	}
	// The rate card is the COD residual's recovery source (ratecard.json, never
	// truth). Absent on periods without one; non-COD breaks never read it.
	rc, _ := feeds.RateCard(root, world, period)

	f := agentclient.RecordedInvestigateFile{
		SchemaVersion: agentclient.InvestigateRecordedSchemaVersion,
		World:         world,
		Period:        period,
		Resolutions:   make([]agentclient.RecordedResolution, 0, len(breaks)),
	}
	for _, b := range breaks {
		if b.Kind == reconcile.KindCODReceivableResidual {
			f.Resolutions = append(f.Resolutions, recoverCODBreak(b, er.raw.CourierFeed, rc))
			continue
		}
		f.Resolutions = append(f.Resolutions, recoverBreak(b, er.skippedEvents, payOrder, rates))
	}
	return f, nil
}

// recoverCODBreak derives the recorded resolution for a COD residual break
// (ROADMAP §8.3) from snapshotted agent-input only (the courier feed + the rate
// card, NEVER truth). It decomposes the remittance's deductions through the SAME
// shared rule the rto-fee policy uses: a rate-card-backed RTO charge becomes an
// rto_fee posting (its net/gst split from the deducted gross via the canonical
// gstsplit, so it equals truth to the paise); an unverified deduction (the weight
// dispute) is escalated with the precise reason and the document to request. The
// two compose in ONE resolution — book what is provable, escalate what is not.
func recoverCODBreak(b reconcile.Break, courier ingest.RawCourierFeed, rc feeds.RateCardFile) agentclient.RecordedResolution {
	key := breakKey(b)
	channel := courier.Channel
	status := make(map[string]string, len(courier.Shipments))
	for _, s := range courier.Shipments {
		status[s.ID] = s.Status
	}

	var postings []agentclient.RecordedPosting
	var booked int
	var escalations []string
	for _, rm := range courier.Remittances {
		for _, d := range rm.Deductions {
			v := policychecks.ClassifyDeduction(policychecks.Deduction{
				ID: d.ID, Code: d.Code, ShipmentID: d.ShipmentID,
				Amount: d.Amount, ShipmentStatus: status[d.ShipmentID],
			}, channel, rc)
			if !v.Backed || v.EntryType != "rto_fee" {
				escalations = append(escalations, fmt.Sprintf("%s %s: %s", d.Code, d.Amount, v.Note))
				continue
			}
			rate, err := strconv.Atoi(v.GSTRate)
			if err != nil || rate <= 0 {
				escalations = append(escalations, fmt.Sprintf("%s %s: rate-card gst rate %q unusable", d.Code, d.Amount, v.GSTRate))
				continue
			}
			net, gst := gstsplit.SplitInclusive(d.Amount, rate)
			postings = append(postings, agentclient.RecordedPosting{
				EventID:   d.ID,
				EntryType: "rto_fee",
				Params: map[string]int64{
					"net":         net.Paise(),
					"gst":         gst.Paise(),
					"shipment_id": 0,
				},
			})
			booked++
		}
	}

	res := agentclient.RecordedResolution{
		BreakKey:  key,
		ToolsUsed: []string{"ratecard.fetch", "courier.fetch"},
	}
	if booked > 0 {
		res.Resolution = postings
		res.Rationale = fmt.Sprintf("cod-receivable residual %s decomposed against the remittance: booked %d rate-card-backed rto_fee deduction(s) (RTO confirmed on the courier feed)",
			b.Actual.Sub(b.Expected), booked)
	}
	if len(escalations) > 0 {
		res.Escalate = true
		res.Reason = "unverified courier deduction(s) with no rate-card basis — request the courier's supporting document: " + joinReasons(escalations)
	}
	return res
}

// joinReasons concatenates escalation reasons deterministically (in feed order).
func joinReasons(rs []string) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += "; "
		}
		out += r
	}
	return out
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
		c, ok, _ := posting.Classify(restored)
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
		// Distinguish the PARTIAL-refund residual: the unbooked refunds are visible,
		// but their intent (line-item return vs goodwill credit) is a classify-side
		// judgment — no investigate posting may guess it (the rule engine's partial
		// guard makes the rate-restored re-classify miss too, by design).
		var partials int
		for _, ev := range skipped {
			if ev.Type == ingest.EventRefund && ev.ParentAmount != nil {
				partials++
			}
		}
		reason := "could not recover an unbooked refund explaining the receivable residual"
		if partials > 0 {
			reason = fmt.Sprintf("residual traces to %d PARTIAL refund(s) whose intent (return vs goodwill credit) is unrecoverable from the snapshot — escalating to a human, never guessing", partials)
		}
		return agentclient.RecordedResolution{
			BreakKey:  key,
			Escalate:  true,
			Reason:    reason,
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
