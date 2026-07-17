package score

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// tline builds a truth.Line for table rows.
func tline(side, account string, paise int64) truth.Line {
	return truth.Line{Side: truth.Side(side), Account: account, Amount: money.FromPaise(paise)}
}

// pline builds a produced score.Line for table rows.
func pline(side, account string, paise int64) Line {
	return Line{Side: side, Account: account, Amount: money.FromPaise(paise)}
}

// saleTruth/saleProduced build a matching truth/produced dtc_sale pair for an
// event, so tests can perturb one side and assert the diff.
func saleTruth(eventID string, gross, net, gst int64) truth.Entry {
	return truth.Entry{
		EntryType: "dtc_sale", EventID: eventID,
		Lines: []truth.Line{
			tline("Dr", "assets/razorpay-settlement-receivable", gross),
			tline("Cr", "income/product-sales", net),
			tline("Cr", "liabilities/gst-output-payable", gst),
		},
	}
}

func saleProduced(eventID string, gross, net, gst int64) Produced {
	return Produced{
		EntryType: "dtc_sale", EventID: eventID,
		Lines: []Line{
			pline("Dr", "assets/razorpay-settlement-receivable", gross),
			pline("Cr", "income/product-sales", net),
			pline("Cr", "liabilities/gst-output-payable", gst),
		},
	}
}

// TestScorePerfect: produced exactly equals truth for every event -> 100%,
// trial balance matches, no errors, IsPerfect.
func TestScorePerfect(t *testing.T) {
	gl := truth.GL{Entries: []truth.Entry{
		saleTruth("pay_1", 11800, 10000, 1800),
		saleTruth("pay_2", 11200, 10000, 1200),
	}}
	produced := []Produced{
		// Deliberately out of truth order to prove matching is by event id.
		saleProduced("pay_2", 11200, 10000, 1200),
		saleProduced("pay_1", 11800, 10000, 1800),
	}
	r := Score(produced, gl)
	if r.Percent() != 100 {
		t.Errorf("Percent = %d, want 100", r.Percent())
	}
	if !r.IsPerfect() {
		t.Errorf("IsPerfect = false, want true; errors=%v", r.Errors)
	}
	if !r.TrialBalanceMatches {
		t.Errorf("TrialBalanceMatches = false, want true")
	}
	if len(r.Errors) != 0 {
		t.Errorf("errors = %v, want none", r.Errors)
	}
}

// TestScoreLineOrderIndependent: the produced entry posts the same lines in a
// different order; it must still match (lines compared as a set).
func TestScoreLineOrderIndependent(t *testing.T) {
	gl := truth.GL{Entries: []truth.Entry{saleTruth("pay_1", 11800, 10000, 1800)}}
	produced := []Produced{{
		EntryType: "dtc_sale", EventID: "pay_1",
		Lines: []Line{
			pline("Cr", "liabilities/gst-output-payable", 1800),
			pline("Cr", "income/product-sales", 10000),
			pline("Dr", "assets/razorpay-settlement-receivable", 11800),
		},
	}}
	if r := Score(produced, gl); !r.IsPerfect() {
		t.Errorf("line-reordered entry not matched; errors=%v", r.Errors)
	}
}

// TestScoreErrorClasses is table-driven over the three error classes.
func TestScoreErrorClasses(t *testing.T) {
	base := truth.GL{Entries: []truth.Entry{saleTruth("pay_1", 11800, 10000, 1800)}}

	tests := []struct {
		name        string
		gl          truth.GL
		produced    []Produced
		wantClass   ErrorClass
		wantTotal   int
		wantCorr    int
		wantPct     int
		wantTBmatch bool
	}{
		{
			name:      "missing (rule miss, skipped)",
			gl:        base,
			produced:  nil, // event skipped
			wantClass: ErrMissing, wantTotal: 1, wantCorr: 0, wantPct: 0, wantTBmatch: false,
		},
		{
			name: "wrong amount",
			gl:   base,
			// gst off by one paise; net+gst no longer == gross so TB also breaks.
			produced:  []Produced{saleProduced("pay_1", 11800, 10000, 1801)},
			wantClass: ErrWrong, wantTotal: 1, wantCorr: 0, wantPct: 0, wantTBmatch: false,
		},
		{
			name: "wrong entry type",
			gl:   base,
			produced: []Produced{{
				EntryType: "refund_reversal", EventID: "pay_1",
				Lines: saleProduced("pay_1", 11800, 10000, 1800).Lines,
			}},
			wantClass: ErrWrong, wantTotal: 1, wantCorr: 0, wantPct: 0, wantTBmatch: true,
		},
		{
			name:      "extra entry",
			gl:        base,
			produced:  []Produced{saleProduced("pay_1", 11800, 10000, 1800), saleProduced("pay_X", 5900, 5000, 900)},
			wantClass: ErrExtra, wantTotal: 1, wantCorr: 1, wantPct: 100, wantTBmatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Score(tt.produced, tt.gl)
			if r.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", r.Total, tt.wantTotal)
			}
			if r.Correct != tt.wantCorr {
				t.Errorf("Correct = %d, want %d", r.Correct, tt.wantCorr)
			}
			if r.Percent() != tt.wantPct {
				t.Errorf("Percent = %d, want %d", r.Percent(), tt.wantPct)
			}
			if r.TrialBalanceMatches != tt.wantTBmatch {
				t.Errorf("TrialBalanceMatches = %v, want %v", r.TrialBalanceMatches, tt.wantTBmatch)
			}
			if len(r.Errors) != 1 {
				t.Fatalf("errors = %v, want exactly 1", r.Errors)
			}
			if r.Errors[0].Class != tt.wantClass {
				t.Errorf("error class = %q, want %q", r.Errors[0].Class, tt.wantClass)
			}
			if r.IsPerfect() {
				t.Errorf("IsPerfect = true, want false")
			}
		})
	}
}

// TestScoreEmptyPeriod: zero truth entries reads as 100% but NOT perfect (there
// is nothing to have gotten right), guarding the empty-denominator path.
func TestScoreEmptyPeriod(t *testing.T) {
	r := Score(nil, truth.GL{})
	if r.Percent() != 100 {
		t.Errorf("Percent on empty = %d, want 100", r.Percent())
	}
	if r.IsPerfect() {
		t.Errorf("IsPerfect on empty = true, want false")
	}
}

// TestScoreErrorsDeterministicOrder: errors are sorted by event id regardless of
// input order, so error records are byte-stable across runs (SPEC §9).
func TestScoreErrorsDeterministicOrder(t *testing.T) {
	gl := truth.GL{Entries: []truth.Entry{
		saleTruth("pay_c", 11800, 10000, 1800),
		saleTruth("pay_a", 11800, 10000, 1800),
		saleTruth("pay_b", 11800, 10000, 1800),
	}}
	// All missing (none produced) -> three errors, must come out a,b,c.
	r := Score(nil, gl)
	if len(r.Errors) != 3 {
		t.Fatalf("errors = %v, want 3", r.Errors)
	}
	for i, want := range []string{"pay_a", "pay_b", "pay_c"} {
		if r.Errors[i].EventID != want {
			t.Errorf("errors[%d].EventID = %q, want %q", i, r.Errors[i].EventID, want)
		}
	}
}
