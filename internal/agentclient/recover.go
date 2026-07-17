package agentclient

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/gstsplit"
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery/policychecks"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// recover.go is the DETERMINISTIC generator that builds the committed
// classify.recorded.json for a period (SPEC §2, §12). It models exactly what the
// §8 classify agent does on a rule miss — "fetch the order, recover the missing
// tax metadata, pick the entry type" — but with no LLM: for each event the rule
// engine MISSED because its gst_rate was stripped, it reads the true rate from the
// matching order in orders.json (the legitimate recovery source, NOT truth) and
// produces the SAME {entry_type: dtc_sale, params} the rule engine would have
// produced had the rate been present.
//
// Drift-proof by construction: the misses are found by running the REAL rule
// engine (posting.Classify), and the GST split uses the REAL shared
// gstsplit.SplitInclusive — the same two functions the deterministic spine and the
// seeder's truth GL use — so a recovered response equals truth to the paise, and
// replaying it raises the score to ~100% exactly. This package NEVER imports
// internal/truth (the truth-isolation guard enforces it).

// orderFetchTool is the read-only tool name recorded in a recovered response's
// tools_used and in its trace (SPEC §8 "tools used"): the agent fetched the order
// to recover the missing rate.
const orderFetchTool = "orders.fetch"

// GenerateRecorded builds the recorded-response fixture for (world, period) under
// root by recovering every rule-missed payment from orders.json. It is the
// reproducible, reviewable generator behind the committed classify.recorded.json:
// running it on the committed hard period reproduces the committed file byte-for-
// byte (a test asserts it).
//
// It ingests + normalizes the period, runs the real rule engine over every event,
// and for each MISS recovers a recorded response (see recoverMiss). A miss the
// generator cannot recover (no matching order, or an order with no usable rate) is
// recorded as a deterministic {unclassifiable, reason} escalation rather than
// dropped — so replay reproduces the escalation and the operator sees it.
func GenerateRecorded(root, world, period string) (RecordedFile, error) {
	_, events, err := ingest.IngestAndNormalize(root, world, period)
	if err != nil {
		return RecordedFile{}, err
	}
	orders, err := feeds.Orders(root, world, period)
	if err != nil {
		return RecordedFile{}, err
	}

	f := RecordedFile{
		SchemaVersion: RecordedSchemaVersion,
		World:         world,
		Period:        period,
		Responses:     make([]RecordedResponse, 0),
	}
	// payment id -> order id, for the refund -> payment -> order walk the
	// partial-refund recovery needs (a refund object carries no order_id).
	payOrder := make(map[string]string)
	for _, ev := range events {
		if ev.Type == ingest.EventPayment {
			payOrder[ev.ID] = ev.Links.OrderID
		}
	}
	for _, ev := range events {
		_, ok, reason := posting.Classify(ev)
		if ok {
			continue // the rule engine handled it; the agent is not consulted.
		}
		f.Responses = append(f.Responses, recoverMiss(ev, reason, orders, payOrder))
	}
	f.sortResponses()
	return f, nil
}

// recoverMiss produces the recorded response for one rule-missed event ev. In the
// v1 hard period every miss is a PAYMENT whose gst_rate was stripped; the agent
// fetches its order, recovers the true rate, and books the dtc_sale. An event the
// generator cannot recover (an unexpected miss type, no matching order, or an
// order without a usable rate) becomes a recorded escalation with a clear reason
// (the rule-miss reason is folded in for context), so the fixture is honest about
// what could not be recovered.
func recoverMiss(ev ingest.NormalizedEvent, missReason string, orders map[string]feeds.OrderInfo, payOrder map[string]string) RecordedResponse {
	// PARTIAL refund (the judgment world): decide via the order's line items.
	if ev.Type == ingest.EventRefund && ev.ParentAmount != nil {
		return recoverPartialRefund(ev, orders, payOrder)
	}
	if ev.Type != ingest.EventPayment {
		return escalation(ev.ID, fmt.Sprintf("rule miss %q on %s event is not a payment the classify agent recovers in v1", missReason, ev.Type))
	}

	o, ok := orders[ev.Links.OrderID]
	if !ok {
		return escalation(ev.ID, fmt.Sprintf("no order %q to recover gst_rate from (rule miss: %s)", ev.Links.OrderID, missReason))
	}
	rate, ok := parseRatePercent(o.GSTRate)
	if !ok || rate <= 0 {
		return escalation(ev.ID, fmt.Sprintf("order %q has no usable gst_rate %q (rule miss: %s)", ev.Links.OrderID, o.GSTRate, missReason))
	}

	// Recovered exactly as classifyPayment would have, had the rate been present:
	// gross = the payment amount; (net, gst) = the shared inclusive split at the
	// recovered rate; params keyed to dtc_sale with the tx_param zero placeholder.
	gross := ev.Amount
	net, gst := gstsplit.SplitInclusive(gross, rate)
	return RecordedResponse{
		EventID:   ev.ID,
		EntryType: "dtc_sale",
		Params: map[string]int64{
			"gross":      gross.Paise(),
			"net":        net.Paise(),
			"gst":        gst.Paise(),
			"payment_id": 0, // tx_param placeholder; the id string travels on the entry's TxID
		},
		Rationale: fmt.Sprintf("payment %s arrived with no gst_rate; fetched order %s, recovered gst_rate=%d%%, and booked the dtc_sale at the recovered rate",
			ev.ID, ev.Links.OrderID, rate),
		ToolsUsed: []string{orderFetchTool},
	}
}

// recoverPartialRefund decides one partial refund the way the agent's policy
// does, against the order's line items (the matching substrate):
//
//  1. an ops ANNOTATION (notes.reason, e.g. "goodwill") wins: a manually
//     annotated credit is a human/policy call — escalate, never book;
//  2. exactly ONE line item (or pair, capped at pairs) summing to the refund is
//     strong evidence of that item returned — book refund_reversal at the
//     matched item's rate, citing the item;
//  3. ambiguity (several matches) or NO match — escalate; the agent never
//     guesses, and an unexplained partial credit is exactly what a human review
//     queue is for.
//
// The walk is refund -> payment -> order (a refund carries no order_id), all
// from snapshotted agent inputs — never truth.
func recoverPartialRefund(ev ingest.NormalizedEvent, orders map[string]feeds.OrderInfo, payOrder map[string]string) RecordedResponse {
	if ev.Notes != nil && ev.Notes.Reason != "" {
		return escalation(ev.ID, fmt.Sprintf(
			"partial refund %s is annotated %q — a goodwill/manual credit is a human policy call, not an agent booking", ev.ID, ev.Notes.Reason))
	}
	orderID := payOrder[ev.Links.PaymentID]
	o, ok := orders[orderID]
	if !ok || len(o.Items) == 0 {
		return escalation(ev.ID, fmt.Sprintf("partial refund %s has no order line items to match against", ev.ID))
	}

	// One matcher, no drift: the SAME pure matcher the bundler's policy layer
	// runs (policychecks.MatchLineItems), projected onto this generator's
	// (rate, cite) shape. The cite strings reproduce the committed fixtures
	// byte-for-byte: "<order>/<path>" plus the matched item's SKU for a single.
	type match struct {
		rate int
		cite string
	}
	var matches []match
	for _, c := range policychecks.MatchLineItems(ev.Amount, orderID, o.Items) {
		if c.Kind == policychecks.CandidateNoMatch {
			continue
		}
		rate, ok := parseRatePercent(c.GSTRate)
		if !ok || rate <= 0 {
			continue
		}
		cite := c.Source.Object + "/" + c.Source.Path
		if c.Kind == policychecks.CandidateItemMatch {
			cite = fmt.Sprintf("%s (%s)", cite, o.Items[c.Items[0]].SKU)
		}
		matches = append(matches, match{rate: rate, cite: cite})
	}

	switch len(matches) {
	case 1:
		net, gst := gstsplit.SplitInclusive(ev.Amount, matches[0].rate)
		return RecordedResponse{
			EventID:   ev.ID,
			EntryType: "refund_reversal",
			Params: map[string]int64{
				"net":       net.Paise(),
				"gst":       gst.Paise(),
				"refund_id": 0,
			},
			Rationale: fmt.Sprintf("partial refund %s of %s equals line item %s — booked the line-item return as a refund_reversal at %d%%",
				ev.ID, ev.Amount, matches[0].cite, matches[0].rate),
			ToolsUsed: []string{orderFetchTool},
		}
	case 0:
		return escalation(ev.ID, fmt.Sprintf(
			"partial refund %s of %s matches no line item or pair of order %s — unexplained partial credit needs a human", ev.ID, ev.Amount, orderID))
	default:
		return escalation(ev.ID, fmt.Sprintf(
			"partial refund %s matches %d candidate item sets of order %s — ambiguous, needs a human", ev.ID, len(matches), orderID))
	}
}

// escalation builds a recorded {unclassifiable, reason} response, recording that
// the agent could not recover the event (it still consulted the order tool, so
// tools_used records the attempt). Replay reproduces it as an escalation.
func escalation(eventID, reason string) RecordedResponse {
	return RecordedResponse{
		EventID:        eventID,
		Unclassifiable: true,
		Reason:         reason,
		ToolsUsed:      []string{orderFetchTool},
	}
}

// SummarizeEvent projects a normalized event onto the EventSummary the §8 agent
// sees (SPEC §8). It is exported so the orchestrator builds the same summary it
// would send to the agent — the input the trace records — without re-deriving the
// projection. money stays paise; the (possibly empty) gst_rate is carried so the
// trace shows the event arrived without it.
func SummarizeEvent(ev ingest.NormalizedEvent) EventSummary {
	s := EventSummary{
		EventID: ev.ID,
		Type:    string(ev.Type),
		Amount:  ev.Amount,
		OrderID: ev.Links.OrderID,
	}
	if ev.Notes != nil {
		s.GSTRate = ev.Notes.GSTRate
		s.SKU = ev.Notes.SKU
	}
	return s
}

// parseRatePercent parses a GST rate string ("18", "12", "5") into an int in
// integer space only — no float paths — mirroring posting.parseRatePercent and
// the seeder's gstRatePercentOf so the recovered rate matches the rule engine's.
// ok is false for an empty string or any non-digit byte.
func parseRatePercent(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// compile-time assertion that the recovered params are integer paise (int64),
// never float — the money invariant at the recovery seam (SPEC §1).
var _ = func(m money.Money) int64 { return m.Paise() }
