package posting

import (
	"encoding/json"
	"testing"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/money"
)

// p is a tiny helper to take the address of a money value for the *money.Money
// fields on NormalizedEvent (Fee/Tax) in table rows.
func p(m money.Money) *money.Money { return &m }

// notes builds a *ingest.Notes with the given gst_rate (sku is irrelevant to
// classification). An empty string yields a Notes with no rate (a miss source).
func notes(rate string) *ingest.Notes { return &ingest.Notes{SKU: "SERUM-30", GSTRate: rate} }

// settlementRaw builds the canonical raw object the settlement rule reads the UTR
// out of, matching ingest's re-marshal of a raw settlement.
func settlementRaw(t *testing.T, utr string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(struct {
		Entity string `json:"entity"`
		ID     string `json:"id"`
		UTR    string `json:"utr"`
	}{Entity: "settlement", ID: "setl_X", UTR: utr})
	if err != nil {
		t.Fatalf("marshal settlement raw: %v", err)
	}
	return b
}

// TestClassifyMatches is the table-driven happy path: one row per entry type,
// asserting the chosen EntryType, the full Params map (integer paise), the IK
// scheme, and the TxID. The GST splits are hand-checked against the canonical
// inclusive formula so the test pins the byte-identical-to-truth requirement.
func TestClassifyMatches(t *testing.T) {
	tests := []struct {
		name      string
		ev        ingest.NormalizedEvent
		wantType  string
		wantIK    string
		wantTxID  string
		wantTs    int64
		wantParam map[string]int64
	}{
		{
			// 328117 @ 5% -> net 312492, gst 15625 (matches the committed truth GL).
			name: "payment -> dtc_sale",
			ev: ingest.NormalizedEvent{
				ID: "pay_A1", Type: ingest.EventPayment, TS: 100,
				Amount: money.FromPaise(328117), Fee: p(money.FromPaise(6562)), Tax: p(money.FromPaise(1181)),
				Notes: notes("5"),
			},
			wantType: "dtc_sale", wantIK: "sale:pay_A1", wantTxID: "pay_A1", wantTs: 100,
			wantParam: map[string]int64{"gross": 328117, "net": 312492, "gst": 15625, "payment_id": 0},
		},
		{
			// net_deposit 90000 + fee 2000 + tax 360 -> gross 92360.
			name: "settlement -> razorpay_settlement",
			ev: ingest.NormalizedEvent{
				ID: "setl_B1", Type: ingest.EventSettlement, TS: 200,
				Amount: money.FromPaise(90000), Fee: p(money.FromPaise(2000)), Tax: p(money.FromPaise(360)),
				Raw: settlementRaw(t, "UTRB1"),
			},
			wantType: "razorpay_settlement", wantIK: "settle:setl_B1", wantTxID: "UTRB1", wantTs: 200,
			wantParam: map[string]int64{"net_deposit": 90000, "fee": 2000, "gst_on_fee": 360, "gross": 92360, "bank_tx_id": 0},
		},
		{
			// 11800 @ 18% -> net 10000, gst 1800.
			name: "refund -> refund_reversal",
			ev: ingest.NormalizedEvent{
				ID: "rfnd_C1", Type: ingest.EventRefund, TS: 300,
				Amount: money.FromPaise(11800), Notes: notes("18"),
			},
			wantType: "refund_reversal", wantIK: "refund:rfnd_C1", wantTxID: "rfnd_C1", wantTs: 300,
			wantParam: map[string]int64{"net": 10000, "gst": 1800, "refund_id": 0},
		},
		{
			// 11200 @ 12% -> net 10000, gst 1200.
			name: "dispute -> chargeback_loss",
			ev: ingest.NormalizedEvent{
				ID: "disp_D1", Type: ingest.EventDispute, TS: 400,
				Amount: money.FromPaise(11200), Notes: notes("12"),
			},
			wantType: "chargeback_loss", wantIK: "dispute:disp_D1", wantTxID: "disp_D1", wantTs: 400,
			wantParam: map[string]int64{"net": 10000, "gst": 1200, "dispute_id": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok, reason := Classify(tt.ev)
			if !ok {
				t.Fatalf("Classify returned miss with reason %q, want a match", reason)
			}
			if reason != "" {
				t.Errorf("reason on a match = %q, want empty", reason)
			}
			if c.EntryType != tt.wantType {
				t.Errorf("EntryType = %q, want %q", c.EntryType, tt.wantType)
			}
			if c.IK != tt.wantIK {
				t.Errorf("IK = %q, want %q", c.IK, tt.wantIK)
			}
			if c.TxID != tt.wantTxID {
				t.Errorf("TxID = %q, want %q", c.TxID, tt.wantTxID)
			}
			if c.Ts != tt.wantTs {
				t.Errorf("Ts = %d, want %d", c.Ts, tt.wantTs)
			}
			if len(c.Params) != len(tt.wantParam) {
				t.Fatalf("Params has %d keys %v, want %d %v", len(c.Params), c.Params, len(tt.wantParam), tt.wantParam)
			}
			for k, want := range tt.wantParam {
				got, present := c.Params[k]
				if !present {
					t.Errorf("Params missing key %q", k)
					continue
				}
				if got.Paise() != want {
					t.Errorf("Params[%q] = %d, want %d", k, got.Paise(), want)
				}
			}
			// Exactness invariant for the GST-split entry types: net+gst == gross.
			switch c.EntryType {
			case "dtc_sale":
				if c.Params["net"].Add(c.Params["gst"]) != c.Params["gross"] {
					t.Errorf("dtc_sale net+gst != gross")
				}
			case "razorpay_settlement":
				sum := c.Params["net_deposit"].Add(c.Params["fee"]).Add(c.Params["gst_on_fee"])
				if sum != c.Params["gross"] {
					t.Errorf("settlement net_deposit+fee+gst_on_fee != gross")
				}
			}
		})
	}
}

// TestClassifyMisses is the table-driven miss path: each row is an event a rule
// cannot book, and the classifier must return ok=false with a non-empty reason
// rather than crash. The synthetic missing-gst_rate payment is the headline case
// from the SPEC (Phase 4: unmatched events are flagged and skipped).
func TestClassifyMisses(t *testing.T) {
	tests := []struct {
		name string
		ev   ingest.NormalizedEvent
	}{
		{
			name: "payment with no notes (missing gst_rate)",
			ev: ingest.NormalizedEvent{
				ID: "pay_NOGST", Type: ingest.EventPayment, TS: 1, Amount: money.FromPaise(11800),
			},
		},
		{
			name: "payment with empty gst_rate",
			ev: ingest.NormalizedEvent{
				ID: "pay_EMPTY", Type: ingest.EventPayment, TS: 1, Amount: money.FromPaise(11800),
				Notes: notes(""),
			},
		},
		{
			name: "payment with non-numeric gst_rate",
			ev: ingest.NormalizedEvent{
				ID: "pay_BAD", Type: ingest.EventPayment, TS: 1, Amount: money.FromPaise(11800),
				Notes: notes("eighteen"),
			},
		},
		{
			name: "payment with zero gst_rate",
			ev: ingest.NormalizedEvent{
				ID: "pay_ZERO", Type: ingest.EventPayment, TS: 1, Amount: money.FromPaise(11800),
				Notes: notes("0"),
			},
		},
		{
			name: "refund with missing gst_rate",
			ev: ingest.NormalizedEvent{
				ID: "rfnd_NOGST", Type: ingest.EventRefund, TS: 1, Amount: money.FromPaise(11800),
			},
		},
		{
			name: "dispute with missing gst_rate",
			ev: ingest.NormalizedEvent{
				ID: "disp_NOGST", Type: ingest.EventDispute, TS: 1, Amount: money.FromPaise(11800),
			},
		},
		{
			name: "settlement missing fee/tax",
			ev: ingest.NormalizedEvent{
				ID: "setl_NOFEE", Type: ingest.EventSettlement, TS: 1, Amount: money.FromPaise(90000),
				Raw: settlementRaw(t, "UTRX"),
			},
		},
		{
			name: "settlement missing UTR",
			ev: ingest.NormalizedEvent{
				ID: "setl_NOUTR", Type: ingest.EventSettlement, TS: 1, Amount: money.FromPaise(90000),
				Fee: p(money.FromPaise(2000)), Tax: p(money.FromPaise(360)),
				Raw: settlementRaw(t, ""),
			},
		},
		{
			name: "unknown event type",
			ev: ingest.NormalizedEvent{
				ID: "weird_1", Type: ingest.EventType("payout"), TS: 1, Amount: money.FromPaise(100),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok, reason := Classify(tt.ev)
			if ok {
				t.Fatalf("Classify returned a match %+v, want a miss", c)
			}
			if c != nil {
				t.Errorf("Classification = %+v on a miss, want nil", c)
			}
			if reason == "" {
				t.Errorf("miss reason is empty, want a human-readable reason")
			}
		})
	}
}

// TestClassifyDoesNotPanicOnMissingMetadata is the explicit "never crash" guard:
// the events that would panic gstsplit.SplitInclusive (zero/garbage rate) must be
// caught as misses, so Classify returns normally for every one of them.
func TestClassifyDoesNotPanicOnMissingMetadata(t *testing.T) {
	bad := []ingest.NormalizedEvent{
		{ID: "p0", Type: ingest.EventPayment, Amount: money.FromPaise(100), Notes: notes("0")},
		{ID: "p1", Type: ingest.EventPayment, Amount: money.FromPaise(100)},
		{ID: "r0", Type: ingest.EventRefund, Amount: money.FromPaise(100), Notes: notes("x")},
		{ID: "d0", Type: ingest.EventDispute, Amount: money.FromPaise(100)},
	}
	for _, ev := range bad {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Classify(%s) panicked: %v", ev.ID, r)
				}
			}()
			if _, ok, _ := Classify(ev); ok {
				t.Errorf("Classify(%s) matched, want a clean miss", ev.ID)
			}
		}()
	}
}
