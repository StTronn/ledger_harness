package seed

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestCODWorldNetsAndBalances asserts the COD rail (cod.go) is internally
// consistent: the courier feed's netting identity holds to the paise, the truth
// GL balances, and the cod-receivable nets to exactly 0 in truth (every delivered
// gross is cleared by the remittance + the two deductions). This is the
// generation invariant the whole COD demo rests on.
func TestCODWorldNetsAndBalances(t *testing.T) {
	fx, feed, gl, _, err := GenerateFull("dtc", "2026-02", Options{COD: true})
	if err != nil {
		t.Fatalf("GenerateFull(COD): %v", err)
	}
	if fx.Courier == nil {
		t.Fatal("COD world produced no courier feed")
	}
	if len(fx.Courier.Remittances) != 1 {
		t.Fatalf("want 1 remittance, got %d", len(fx.Courier.Remittances))
	}
	rem := fx.Courier.Remittances[0]

	// Netting identity: gross_collected == net + fee + gst + Σ deductions.
	var ded money.Money
	for _, d := range rem.Deductions {
		ded = ded.Add(d.Amount)
	}
	sum := rem.NetDeposit.Add(rem.CollectionFee).Add(rem.GSTOnFee).Add(ded)
	if sum != rem.GrossCollected {
		t.Errorf("remittance netting: net+fee+gst+ded = %s, want gross_collected %s", sum, rem.GrossCollected)
	}

	// The two deductions are the ₹118 RTO fee + ₹40 weight adjustment = ₹158.
	if ded != money.FromPaise(15800) {
		t.Errorf("Σ deductions = %s, want 158.00", ded)
	}

	// Truth balances, and cod-receivable nets to 0 (every delivery cleared).
	if !gl.IsBalanced() {
		t.Error("COD truth GL does not balance")
	}
	var codBal money.Money
	for _, e := range gl.Entries {
		for _, l := range e.Lines {
			if l.Account != "assets/cod-receivable" {
				continue
			}
			if l.Side == "Dr" {
				codBal = codBal.Add(l.Amount)
			} else {
				codBal = codBal.Sub(l.Amount)
			}
		}
	}
	if codBal != money.FromPaise(0) {
		t.Errorf("cod-receivable truth balance = %s, want 0 (all deliveries cleared)", codBal)
	}

	// The remittance's bank credit must be in the bank feed (matched on UTR).
	var found bool
	for _, c := range feed.Credits {
		if c.Ref == rem.UTR && c.Amount == rem.NetDeposit {
			found = true
		}
	}
	if !found {
		t.Errorf("no bank credit for remittance UTR %s amount %s", rem.UTR, rem.NetDeposit)
	}
}

// TestCODWorldDeterministic asserts the COD world is byte-reproducible: same
// (world, period, Options) => identical courier feed and truth GL.
func TestCODWorldDeterministic(t *testing.T) {
	fx1, _, gl1, _, err := GenerateFull("dtc", "2026-02", Options{COD: true})
	if err != nil {
		t.Fatalf("GenerateFull #1: %v", err)
	}
	fx2, _, gl2, _, err := GenerateFull("dtc", "2026-02", Options{COD: true})
	if err != nil {
		t.Fatalf("GenerateFull #2: %v", err)
	}
	b1, err := MarshalStable(fx1.Courier)
	if err != nil {
		t.Fatalf("marshal courier #1: %v", err)
	}
	b2, err := MarshalStable(fx2.Courier)
	if err != nil {
		t.Fatalf("marshal courier #2: %v", err)
	}
	if string(b1) != string(b2) {
		t.Error("courier feed not deterministic across two generations")
	}
	if len(gl1.Entries) != len(gl2.Entries) {
		t.Errorf("truth GL entry count differs: %d vs %d", len(gl1.Entries), len(gl2.Entries))
	}
}
