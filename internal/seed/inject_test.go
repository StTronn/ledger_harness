package seed

import (
	"strings"
	"testing"

	"github.com/razorpay/close-agent/internal/truth"
)

// TestInjectRefundInBatchKeepsTruth is the Phase-5 seeded-break invariant (SPEC
// §5, §7, §12): the refund-in-batch injection perturbs ONLY the agent-input
// fixtures. The hidden truth GL must still balance AND still include the omitted
// refund's refund_reversal entry, the refund must remain in refunds.json, and the
// settlement's net_deposit must be unchanged — the break is purely that the
// settlement's batch members no longer account for the (refund-reduced) deposit.
func TestInjectRefundInBatchKeepsTruth(t *testing.T) {
	clean, _, cleanGL, _, _, err := GenerateWith("dtc", "2026-05", Options{})
	if err != nil {
		t.Fatalf("clean Generate: %v", err)
	}
	injFx, _, injGL, inj, _, err := GenerateWith("dtc", "2026-05", Options{Inject: InjectRefundInBatch})
	if err != nil {
		t.Fatalf("injected Generate: %v", err)
	}

	if inj.Kind != InjectRefundInBatch || inj.SettlementID == "" || inj.RefundID == "" {
		t.Fatalf("injection result not populated: %+v", inj)
	}

	// 1) Truth GL is byte-equal to the clean GL (the injection never touches truth)
	// and still balances.
	cb, _ := MarshalStable(cleanGL)
	ib, _ := MarshalStable(injGL)
	if string(cb) != string(ib) {
		t.Errorf("injection changed the truth GL; it must be left intact")
	}
	if !injGL.IsBalanced() {
		dr, cr := injGL.SumBySide()
		t.Errorf("injected truth GL does not balance: Dr=%s Cr=%s", dr, cr)
	}

	// 2) Truth GL still includes the omitted refund's refund_reversal entry.
	if !truthHasRefundEntry(injGL.Entries, inj.RefundID) {
		t.Errorf("truth GL is missing the omitted refund %s", inj.RefundID)
	}

	// 3) The refund still exists in refunds.json (only its batch membership changed).
	if !refundsContain(injFx.Refunds, inj.RefundID) {
		t.Errorf("refunds.json no longer contains the omitted refund %s", inj.RefundID)
	}

	// 4) The targeted settlement's net_deposit is unchanged from clean; only its
	// refund_ids dropped the one id. Every other settlement is identical.
	cleanByID := settlementsByID(clean.Settlements)
	for _, s := range injFx.Settlements {
		cs := cleanByID[s.ID]
		if s.Amount != cs.Amount {
			t.Errorf("settlement %s net_deposit changed by injection: %s -> %s", s.ID, cs.Amount, s.Amount)
		}
		if s.ID == inj.SettlementID {
			if len(s.RefundIDs) != len(cs.RefundIDs)-1 {
				t.Errorf("settlement %s refund_ids = %v, want one fewer than clean %v",
					s.ID, s.RefundIDs, cs.RefundIDs)
			}
			if sliceContains(s.RefundIDs, inj.RefundID) {
				t.Errorf("settlement %s still references the dropped refund %s", s.ID, inj.RefundID)
			}
		} else if !equalSlices(s.RefundIDs, cs.RefundIDs) {
			t.Errorf("non-target settlement %s refund_ids changed: %v -> %v", s.ID, cs.RefundIDs, s.RefundIDs)
		}
	}
}

// TestInjectUnbookedRefundKeepsTruth is the Phase-8 seeded-break invariant (SPEC
// §7 check #3, §8, §12): the unbooked-refund injection STRIPS the gst_rate from a
// netted refund and perturbs NOTHING else. The hidden truth GL must still balance
// AND still book the refund's refund_reversal at its true rate; the refund must
// remain in refunds.json (so its id/amount/payment_id stay recoverable) with only
// its gst_rate cleared; its amount, its batch membership, and the settlement's
// net_deposit must all be unchanged — so check #2 (batch-sum) stays green and the
// break is purely that the refund goes UNBOOKED (a check #3 receivable residual).
func TestInjectUnbookedRefundKeepsTruth(t *testing.T) {
	clean, _, cleanGL, _, _, err := GenerateWith("dtc", "2026-05", Options{})
	if err != nil {
		t.Fatalf("clean Generate: %v", err)
	}
	injFx, _, injGL, inj, _, err := GenerateWith("dtc", "2026-05", Options{Inject: InjectUnbookedRefund})
	if err != nil {
		t.Fatalf("injected Generate: %v", err)
	}

	if inj.Kind != InjectUnbookedRefund || inj.SettlementID == "" || inj.RefundID == "" {
		t.Fatalf("injection result not populated: %+v", inj)
	}

	// 1) Truth GL is byte-equal to the clean GL and still balances and still books
	// the unbooked refund (the correct resolution the agent must reproduce).
	cb, _ := MarshalStable(cleanGL)
	ib, _ := MarshalStable(injGL)
	if string(cb) != string(ib) {
		t.Errorf("injection changed the truth GL; it must be left intact")
	}
	if !injGL.IsBalanced() {
		dr, cr := injGL.SumBySide()
		t.Errorf("injected truth GL does not balance: Dr=%s Cr=%s", dr, cr)
	}
	if !truthHasRefundEntry(injGL.Entries, inj.RefundID) {
		t.Errorf("truth GL is missing the unbooked refund %s", inj.RefundID)
	}

	// 2) The refund is still in refunds.json, with ONLY its gst_rate stripped; its
	// amount (and thus its membership economics) are unchanged from clean.
	cleanRef := refundByID(clean.Refunds, inj.RefundID)
	injRef := refundByID(injFx.Refunds, inj.RefundID)
	if injRef == nil {
		t.Fatalf("refunds.json no longer contains the unbooked refund %s", inj.RefundID)
	}
	if injRef.Notes.GSTRate != "" {
		t.Errorf("refund %s gst_rate = %q, want stripped (empty)", inj.RefundID, injRef.Notes.GSTRate)
	}
	if cleanRef == nil || injRef.Amount != cleanRef.Amount {
		t.Errorf("refund %s amount changed by injection", inj.RefundID)
	}
	if injRef.Notes.SKU == "" || injRef.Notes.SKU != cleanRef.Notes.SKU {
		t.Errorf("refund %s sku changed/cleared by injection (only gst_rate should be stripped)", inj.RefundID)
	}

	// 3) Every settlement is byte-identical to clean: net_deposit AND refund_ids are
	// untouched (the refund stays in its batch), so check #2 cannot fire.
	cleanByID := settlementsByID(clean.Settlements)
	for _, s := range injFx.Settlements {
		cs := cleanByID[s.ID]
		if s.Amount != cs.Amount {
			t.Errorf("settlement %s net_deposit changed by injection: %s -> %s", s.ID, cs.Amount, s.Amount)
		}
		if !equalSlices(s.RefundIDs, cs.RefundIDs) {
			t.Errorf("settlement %s refund_ids changed by injection: %v -> %v", s.ID, cs.RefundIDs, s.RefundIDs)
		}
	}
	if !sliceContains(settlementsByID(injFx.Settlements)[inj.SettlementID].RefundIDs, inj.RefundID) {
		t.Errorf("settlement %s no longer lists the unbooked refund %s in its batch", inj.SettlementID, inj.RefundID)
	}
}

// refundByID returns a pointer to the refund with the given id, or nil.
func refundByID(refunds []Refund, id string) *Refund {
	for i := range refunds {
		if refunds[i].ID == id {
			return &refunds[i]
		}
	}
	return nil
}

// TestInjectUnknownIsError asserts an unknown injection kind is a clear error,
// not a silently-clean seed.
func TestInjectUnknownIsError(t *testing.T) {
	_, _, _, _, _, err := GenerateWith("dtc", "2026-05", Options{Inject: Inject("does-not-exist")})
	if err == nil {
		t.Fatal("unknown inject kind did not error")
	}
}

// TestCleanGenerateUnaffectedByInjectPlumbing asserts the no-inject path is
// byte-identical to the legacy Generate, so committed clean fixtures are
// unchanged by the Phase-5 plumbing.
func TestCleanGenerateUnaffectedByInjectPlumbing(t *testing.T) {
	a, fa, ga, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, fb, gb, _, _, err := GenerateWith("dtc", "2026-05", Options{Inject: InjectNone})
	if err != nil {
		t.Fatalf("GenerateWith none: %v", err)
	}
	for _, pair := range []struct {
		name string
		x, y any
	}{
		{"payments", a.Payments, b.Payments},
		{"refunds", a.Refunds, b.Refunds},
		{"settlements", a.Settlements, b.Settlements},
		{"disputes", a.Disputes, b.Disputes},
		{"bank-feed", fa, fb},
		{"truth-gl", ga, gb},
	} {
		bx, _ := MarshalStable(pair.x)
		by, _ := MarshalStable(pair.y)
		if string(bx) != string(by) {
			t.Errorf("%s differs between Generate and GenerateWith(none)", pair.name)
		}
	}
}

// truthHasRefundEntry reports whether the truth GL entries include a
// refund_reversal attributed to the given refund id (its event id / ik), proving
// the omitted refund is still booked in truth after the injection.
func truthHasRefundEntry(entries []truth.Entry, refundID string) bool {
	for _, e := range entries {
		if e.EntryType == "refund_reversal" && (e.EventID == refundID || strings.Contains(e.ID, refundID)) {
			return true
		}
	}
	return false
}

// helpers below operate on the concrete seed/truth types.

func refundsContain(refunds []Refund, id string) bool {
	for _, r := range refunds {
		if r.ID == id {
			return true
		}
	}
	return false
}

func settlementsByID(ss []Settlement) map[string]Settlement {
	m := make(map[string]Settlement, len(ss))
	for _, s := range ss {
		m[s.ID] = s
	}
	return m
}

func sliceContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
