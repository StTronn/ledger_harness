package reconcile

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/money"
)

// TestCODReceivableResidual asserts the COD twin of check #3 (ROADMAP §8.3): a
// non-zero cod-receivable balance raises one cod-receivable-residual break that
// points at the remittance whose deductions explain the gap; a zero balance (no
// COD activity) raises none.
func TestCODReceivableResidual(t *testing.T) {
	feed := ingest.RawCourierFeed{
		Remittances: []ingest.RawRemittance{{ID: "rem_X"}},
	}

	// Non-zero balance -> exactly one COD residual break.
	breaks := Reconcile(Input{
		CODReceivableBalance: money.FromPaise(15800),
		CourierFeed:          feed,
	})
	var cod []Break
	for _, b := range breaks {
		if b.Kind == KindCODReceivableResidual {
			cod = append(cod, b)
		}
	}
	if len(cod) != 1 {
		t.Fatalf("want 1 cod-receivable-residual break, got %d (all: %+v)", len(cod), breaks)
	}
	b := cod[0]
	if b.Check != CheckReceivableClears {
		t.Errorf("check = %d, want %d", b.Check, CheckReceivableClears)
	}
	if b.Actual != money.FromPaise(15800) || b.Expected != money.FromPaise(0) {
		t.Errorf("expected/actual = %s/%s, want 0.00/158.00", b.Expected, b.Actual)
	}
	if b.SettlementID != "rem_X" {
		t.Errorf("break settlement id = %q, want rem_X", b.SettlementID)
	}
	if b.Key() != "check3:cod-receivable-residual:rem_X" {
		t.Errorf("break key = %q", b.Key())
	}

	// Zero balance -> no COD break.
	for _, b := range Reconcile(Input{CODReceivableBalance: money.FromPaise(0), CourierFeed: feed}) {
		if b.Kind == KindCODReceivableResidual {
			t.Errorf("zero balance must not raise a COD residual break, got %+v", b)
		}
	}
}
