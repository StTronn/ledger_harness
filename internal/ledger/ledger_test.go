package ledger

import (
	"errors"
	"testing"

	"github.com/razorpay/close-agent/internal/money"
)

// fakeChart is a hand-built accountSet for tests that should not depend on the
// real playbook file: a fixed set of paths each with a declared normal side.
type fakeChart struct {
	normal map[string]Side
}

func newFakeChart() fakeChart {
	return fakeChart{normal: map[string]Side{
		"assets/bank":                           Debit,
		"assets/razorpay-settlement-receivable": Debit,
		"expense/processor-fees":                Debit,
		"expense/gst-input":                     Debit,
		"expense/chargeback-loss":               Debit,
		"liabilities/gst-output-payable":        Credit,
		"liabilities/dispute-reserve":           Credit,
		"income/product-sales":                  Credit,
		"income/shipping-revenue":               Credit,
		"income/sales-returns":                  Credit,
	}}
}

func (c fakeChart) HasAccount(path string) bool { _, ok := c.normal[path]; return ok }
func (c fakeChart) NormalSide(path string) Side { return c.normal[path] }

// dr/cr build a positive-magnitude line on the given side in paise.
func dr(account string, paise int64) Line {
	return Line{Side: Debit, Account: account, Amount: money.FromPaise(paise)}
}
func cr(account string, paise int64) Line {
	return Line{Side: Credit, Account: account, Amount: money.FromPaise(paise)}
}

// TestPostBalanceOrReject covers the balance-or-reject rule and structural
// validation: a balanced entry posts; everything invalid is rejected with the
// right sentinel AND leaves the ledger unchanged.
func TestPostBalanceOrReject(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		wantErr error // nil => expect success
	}{
		{
			name: "balanced two-line entry posts",
			entry: Entry{Type: "t", IK: "ik1", Lines: []Line{
				dr("assets/bank", 100000),
				cr("income/product-sales", 100000),
			}},
			wantErr: nil,
		},
		{
			name: "balanced multi-line entry posts",
			entry: Entry{Type: "t", IK: "ik2", Lines: []Line{
				dr("assets/bank", 95280),
				dr("expense/processor-fees", 4000),
				dr("expense/gst-input", 720),
				cr("assets/razorpay-settlement-receivable", 100000),
			}},
			wantErr: nil,
		},
		{
			name: "unbalanced is rejected",
			entry: Entry{Type: "t", IK: "bad1", Lines: []Line{
				dr("assets/bank", 100000),
				cr("income/product-sales", 99999),
			}},
			wantErr: ErrUnbalanced,
		},
		{
			name: "unknown account is rejected",
			entry: Entry{Type: "t", IK: "bad2", Lines: []Line{
				dr("assets/nope", 100000),
				cr("income/product-sales", 100000),
			}},
			wantErr: ErrUnknownAccount,
		},
		{
			name: "negative amount is rejected",
			entry: Entry{Type: "t", IK: "bad3", Lines: []Line{
				{Side: Debit, Account: "assets/bank", Amount: money.FromPaise(-100000)},
				cr("income/product-sales", -100000),
			}},
			wantErr: ErrBadLine,
		},
		{
			name: "invalid side is rejected",
			entry: Entry{Type: "t", IK: "bad4", Lines: []Line{
				{Side: "XX", Account: "assets/bank", Amount: money.FromPaise(100000)},
				cr("income/product-sales", 100000),
			}},
			wantErr: ErrBadLine,
		},
		{
			name:    "no lines is rejected",
			entry:   Entry{Type: "t", IK: "bad5", Lines: nil},
			wantErr: ErrBadLine,
		},
		{
			name: "empty IK is rejected",
			entry: Entry{Type: "t", IK: "", Lines: []Line{
				dr("assets/bank", 100000),
				cr("income/product-sales", 100000),
			}},
			wantErr: ErrEmptyIK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lg := New(newFakeChart())
			before := lg.Len()
			_, err := lg.Post(tc.entry)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Post: unexpected error %v", err)
				}
				if lg.Len() != before+1 {
					t.Fatalf("ledger len = %d, want %d (entry should have posted)", lg.Len(), before+1)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Post error = %v, want errors.Is %v", err, tc.wantErr)
			}
			if lg.Len() != before {
				t.Fatalf("ledger len = %d, want %d (rejected entry must not post)", lg.Len(), before)
			}
		})
	}
}

// TestRejectedEntryLeavesLedgerUnchanged posts a good entry, then a bad one, and
// asserts the bad post mutated nothing (entries, IK index, and reports identical).
func TestRejectedEntryLeavesLedgerUnchanged(t *testing.T) {
	lg := New(newFakeChart())
	good := Entry{Type: "t", IK: "good", Lines: []Line{
		dr("assets/bank", 50000), cr("income/product-sales", 50000),
	}}
	if _, err := lg.Post(good); err != nil {
		t.Fatalf("Post good: %v", err)
	}
	tbBefore := lg.TrialBalance()
	lenBefore := lg.Len()

	bad := Entry{Type: "t", IK: "bad", Lines: []Line{
		dr("assets/bank", 1), cr("income/product-sales", 2),
	}}
	if _, err := lg.Post(bad); !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("Post bad: err = %v, want ErrUnbalanced", err)
	}

	if lg.Len() != lenBefore {
		t.Fatalf("len changed after rejected post: %d -> %d", lenBefore, lg.Len())
	}
	tbAfter := lg.TrialBalance()
	if tbAfter.TotalDr != tbBefore.TotalDr || tbAfter.TotalCr != tbBefore.TotalCr {
		t.Fatalf("trial balance changed after rejected post: before %s/%s after %s/%s",
			tbBefore.TotalDr, tbBefore.TotalCr, tbAfter.TotalDr, tbAfter.TotalCr)
	}
	// The conflicting IK must also be absent so a later real "bad" IK could post.
	if _, ok := lg.byIK["bad"]; ok {
		t.Fatalf("rejected entry's IK leaked into the index")
	}
}

// TestIdempotency covers IK behavior: re-posting identical content is a no-op
// returning the existing entry; re-posting the same IK with different content is
// an ErrIKConflict that changes nothing.
func TestIdempotency(t *testing.T) {
	lg := New(newFakeChart())
	e := Entry{Type: "dtc_sale", IK: "evt-1", TxID: "42", Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 100000),
		cr("income/product-sales", 84746),
		cr("liabilities/gst-output-payable", 15254),
	}}

	first, err := lg.Post(e)
	if err != nil {
		t.Fatalf("first Post: %v", err)
	}
	if lg.Len() != 1 {
		t.Fatalf("len = %d after first post, want 1", lg.Len())
	}

	// Identical re-post: no-op, returns the existing entry, len unchanged.
	second, err := lg.Post(e)
	if err != nil {
		t.Fatalf("idempotent re-Post: %v", err)
	}
	if lg.Len() != 1 {
		t.Fatalf("len = %d after idempotent re-post, want 1", lg.Len())
	}
	if !entriesEqual(first, second) {
		t.Fatalf("idempotent re-post returned different entry: %+v vs %+v", first, second)
	}

	// A separately-constructed but content-identical entry is also a no-op.
	clone := Entry{Type: "dtc_sale", IK: "evt-1", TxID: "42", Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 100000),
		cr("income/product-sales", 84746),
		cr("liabilities/gst-output-payable", 15254),
	}}
	if _, err := lg.Post(clone); err != nil {
		t.Fatalf("idempotent clone Post: %v", err)
	}
	if lg.Len() != 1 {
		t.Fatalf("len = %d after clone post, want 1", lg.Len())
	}

	// Conflicting content under the same IK is an error and changes nothing.
	conflictCases := []struct {
		name  string
		entry Entry
	}{
		{"different amount", Entry{Type: "dtc_sale", IK: "evt-1", TxID: "42", Lines: []Line{
			dr("assets/razorpay-settlement-receivable", 100001),
			cr("income/product-sales", 84747),
			cr("liabilities/gst-output-payable", 15254),
		}}},
		{"different account", Entry{Type: "dtc_sale", IK: "evt-1", TxID: "42", Lines: []Line{
			dr("assets/bank", 100000),
			cr("income/product-sales", 84746),
			cr("liabilities/gst-output-payable", 15254),
		}}},
		{"different txid", Entry{Type: "dtc_sale", IK: "evt-1", TxID: "99", Lines: []Line{
			dr("assets/razorpay-settlement-receivable", 100000),
			cr("income/product-sales", 84746),
			cr("liabilities/gst-output-payable", 15254),
		}}},
		{"reordered lines", Entry{Type: "dtc_sale", IK: "evt-1", TxID: "42", Lines: []Line{
			cr("income/product-sales", 84746),
			dr("assets/razorpay-settlement-receivable", 100000),
			cr("liabilities/gst-output-payable", 15254),
		}}},
	}
	for _, cc := range conflictCases {
		t.Run("conflict/"+cc.name, func(t *testing.T) {
			_, err := lg.Post(cc.entry)
			if !errors.Is(err, ErrIKConflict) {
				t.Fatalf("Post conflicting %q: err = %v, want ErrIKConflict", cc.name, err)
			}
			if lg.Len() != 1 {
				t.Fatalf("len = %d after conflict, want 1 (unchanged)", lg.Len())
			}
		})
	}
}

// TestEntriesIsACopy proves reports are pure: mutating the slice returned by
// Entries() (or the caller's original line slice after posting) does not change
// the ledger.
func TestEntriesIsACopy(t *testing.T) {
	lg := New(newFakeChart())
	lines := []Line{dr("assets/bank", 100000), cr("income/product-sales", 100000)}
	e := Entry{Type: "t", IK: "x", Lines: lines}
	if _, err := lg.Post(e); err != nil {
		t.Fatalf("Post: %v", err)
	}

	// Mutating the caller's original slice must not affect the stored entry.
	lines[0].Amount = money.FromPaise(999)

	// Mutating the returned copy must not affect the ledger either.
	got := lg.Entries()
	got[0].Lines[1].Amount = money.FromPaise(777)

	tb := lg.TrialBalance()
	if tb.TotalDr != money.FromPaise(100000) || tb.TotalCr != money.FromPaise(100000) {
		t.Fatalf("ledger mutated through Entries()/caller slice: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
	}
}
