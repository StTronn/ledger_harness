package classifyq

import (
	"path/filepath"
	"testing"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/money"
)

// proposedResult builds a valid proposed Result citing order O with rate "5".
func proposedResult(eventID, order, rate string) Result {
	return Result{
		EventID:   eventID,
		Status:    StatusProposed,
		EntryType: "dtc_sale",
		Recovered: []Recovered{{Field: "gst_rate", Value: rate, Source: Source{Tool: "orders.fetch", Object: order, Path: "notes.gst_rate"}}},
	}
}

// TestWorkerProposesWithProvenance: the stub brain recovers a payment's rate from
// its order and cites the source; a non-payment escalates.
func TestWorkerProposesWithProvenance(t *testing.T) {
	rates := map[string]string{"order_1": "5"}
	pay := classifyOne(WorkItem{EventID: "pay_1", Event: agentclient.EventSummary{EventID: "pay_1", Type: "payment", Amount: money.FromPaise(1000), OrderID: "order_1"}}, rates)
	if !pay.Proposed() || pay.EntryType != "dtc_sale" {
		t.Fatalf("payment should propose a dtc_sale, got %+v", pay)
	}
	rec, ok := findRecovered(pay, "gst_rate")
	if !ok || rec.Value != "5" || rec.Source.Object != "order_1" || rec.Source.Path != "notes.gst_rate" {
		t.Errorf("missing/incorrect provenance citation: %+v", pay.Recovered)
	}
	ref := classifyOne(WorkItem{EventID: "rfnd_1", Event: agentclient.EventSummary{EventID: "rfnd_1", Type: "refund", OrderID: ""}}, rates)
	if ref.Proposed() || ref.Status != StatusEscalated {
		t.Errorf("non-payment should escalate, got %+v", ref)
	}
}

// TestValidateRate covers the citation re-verification: accept a matching citation,
// reject a forged value, an unknown order, and a non-slab rate.
func TestValidateRate(t *testing.T) {
	rates := map[string]string{"order_1": "5", "order_2": "7"} // 7 is not a real slab

	if n, err := ValidateRate(proposedResult("pay_1", "order_1", "5"), rates); err != nil || n != 5 {
		t.Errorf("valid citation rejected: n=%d err=%v", n, err)
	}
	if _, err := ValidateRate(proposedResult("pay_1", "order_1", "18"), rates); err == nil {
		t.Error("forged value (claims 18, order holds 5) should be rejected")
	}
	if _, err := ValidateRate(proposedResult("pay_1", "order_X", "5"), rates); err == nil {
		t.Error("citation to an unknown order should be rejected")
	}
	if _, err := ValidateRate(proposedResult("pay_2", "order_2", "7"), rates); err == nil {
		t.Error("a non-slab rate (7) should be rejected even if the citation matches")
	}
	// no citation at all
	if _, err := ValidateRate(Result{EventID: "x", Status: StatusProposed, EntryType: "dtc_sale"}, rates); err == nil {
		t.Error("a result with no gst_rate citation should be rejected")
	}
}

// TestReviewers covers the auto and recorded review gates.
func TestReviewers(t *testing.T) {
	r := proposedResult("pay_1", "order_1", "5")
	if (AutoReviewer{}).Review(r).Verdict != VerdictApprove {
		t.Error("auto reviewer must approve")
	}
	rec := NewRecordedReviewer(ApprovalsFile{Approvals: []Approval{{EventID: "pay_1", Verdict: VerdictApprove}}})
	if rec.Review(r).Verdict != VerdictApprove {
		t.Error("recorded approve not honored")
	}
	// fail-closed: an unrecorded event is rejected, not approved.
	if d := rec.Review(proposedResult("pay_2", "order_1", "5")); d.Verdict != VerdictReject {
		t.Errorf("unrecorded proposal must fail-closed to reject, got %+v", d)
	}
}

// TestStoreRoundTripStable: stores round-trip and are byte-stable across writes.
func TestStoreRoundTripStable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "results.json")
	in := ResultsFile{SchemaVersion: SchemaVersion, World: "dtc", Period: "2026-04",
		Results: []Result{proposedResult("pay_b", "order_b", "12"), proposedResult("pay_a", "order_a", "5")}}
	if err := WriteResults(p, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadResults(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// sorted by event_id on write
	if got.Results[0].EventID != "pay_a" || got.Results[1].EventID != "pay_b" {
		t.Errorf("results not sorted by event_id: %+v", got.Results)
	}
}
