package ledger

import (
	"testing"

	"github.com/razorpay/close-agent/internal/money"
)

// buildScenario posts a deterministic hand-written set of entries that exercise
// every root and the contra-revenue account, then returns the ledger. It models
// one small, internally-consistent period in which the receivable clears to
// exactly zero:
//
//	sale-1     Dr receivable 118000 ; Cr product-sales 100000 ; Cr gst 18000
//	sale-2     Dr receivable  59000 ; Cr product-sales  50000 ; Cr gst  9000
//	settle-1   Dr bank 113280 ; Dr fees 4000 ; Dr gst-input 720 ; Cr receivable 118000
//	             (settles ONLY sale-1: gross 118000, fee 4000, gst-on-fee 720)
//	refund-1   Dr sales-returns 50000 ; Dr gst 9000 ; Cr receivable 59000
//	             (refunds the UNSETTLED sale-2, clearing its receivable)
//
// Net receivable: Dr 177000 − Cr 177000 = 0. Every entry balances, so the trial
// balance balances and the balance sheet ties to net income exactly.
func buildScenario(t *testing.T) *Ledger {
	t.Helper()
	lg := New(newFakeChart())

	post := func(e Entry) {
		t.Helper()
		if _, err := lg.Post(e); err != nil {
			t.Fatalf("Post %q: %v", e.IK, err)
		}
	}

	// Sale 1: gross 118000 = net 100000 + gst 18000.
	post(Entry{Type: "dtc_sale", IK: "sale-1", TxID: "1001", Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 118000),
		cr("income/product-sales", 100000),
		cr("liabilities/gst-output-payable", 18000),
	}})
	// Sale 2: gross 59000 = net 50000 + gst 9000.
	post(Entry{Type: "dtc_sale", IK: "sale-2", TxID: "1002", Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 59000),
		cr("income/product-sales", 50000),
		cr("liabilities/gst-output-payable", 9000),
	}})
	// Settlement of sale 1 only: gross 118000, fee 4000, gst-on-fee 720,
	// net_deposit = 118000 - 4000 - 720 = 113280.
	post(Entry{Type: "razorpay_settlement", IK: "settle-1", TxID: "2001", Lines: []Line{
		dr("assets/bank", 113280),
		dr("expense/processor-fees", 4000),
		dr("expense/gst-input", 720),
		cr("assets/razorpay-settlement-receivable", 118000),
	}})
	// Refund of the unsettled sale 2: net 50000 + gst 9000 reversed; clears
	// receivable 59000.
	post(Entry{Type: "refund_reversal", IK: "refund-1", TxID: "3001", Lines: []Line{
		dr("income/sales-returns", 50000),
		dr("liabilities/gst-output-payable", 9000),
		cr("assets/razorpay-settlement-receivable", 59000),
	}})

	return lg
}

// TestTrialBalanceExact asserts exact per-account Dr/Cr totals and ΣDr==ΣCr.
func TestTrialBalanceExact(t *testing.T) {
	lg := buildScenario(t)
	tb := lg.TrialBalance()

	// Expected raw Dr/Cr per account (paise), in account-path order.
	want := map[string]struct{ dr, cr int64 }{
		"assets/bank":                           {113280, 0},
		"assets/razorpay-settlement-receivable": {177000, 177000},
		"expense/gst-input":                     {720, 0},
		"expense/processor-fees":                {4000, 0},
		"income/product-sales":                  {0, 150000},
		"income/sales-returns":                  {50000, 0},
		"liabilities/gst-output-payable":        {9000, 27000},
	}

	if len(tb.Rows) != len(want) {
		t.Fatalf("trial balance has %d rows, want %d: %+v", len(tb.Rows), len(want), tb.Rows)
	}
	// Rows must be sorted by account path.
	for i := 1; i < len(tb.Rows); i++ {
		if tb.Rows[i-1].Account >= tb.Rows[i].Account {
			t.Fatalf("rows not sorted: %q before %q", tb.Rows[i-1].Account, tb.Rows[i].Account)
		}
	}
	for _, r := range tb.Rows {
		w, ok := want[r.Account]
		if !ok {
			t.Fatalf("unexpected account %q in trial balance", r.Account)
		}
		if r.Debit.Paise() != w.dr || r.Credit.Paise() != w.cr {
			t.Errorf("account %q: Dr=%d Cr=%d, want Dr=%d Cr=%d",
				r.Account, r.Debit.Paise(), r.Credit.Paise(), w.dr, w.cr)
		}
	}

	// ΣDr and ΣCr must each equal the grand total and be equal to each other.
	// Total Dr = 113280 + 177000 + 720 + 4000 + 0 + 50000 + 9000 = 354000.
	const wantTotal = 354000
	if tb.TotalDr.Paise() != wantTotal || tb.TotalCr.Paise() != wantTotal {
		t.Fatalf("totals Dr=%d Cr=%d, want both %d", tb.TotalDr.Paise(), tb.TotalCr.Paise(), wantTotal)
	}
	if !tb.IsBalanced() {
		t.Fatalf("trial balance not balanced: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
	}
}

// TestBalanceSheetExact asserts asset/liability normal-side balances and totals.
func TestBalanceSheetExact(t *testing.T) {
	lg := buildScenario(t)
	bs := lg.BalanceSheet()

	// Assets (normal Debit, balance = Dr - Cr):
	//   bank        113280
	//   receivable  177000 - 177000 = 0  (cleared)
	// TotalAssets = 113280.
	wantAssets := map[string]int64{
		"assets/bank":                           113280,
		"assets/razorpay-settlement-receivable": 0,
	}
	assertBalances(t, "assets", bs.Assets, wantAssets)
	if bs.TotalAssets.Paise() != 113280 {
		t.Errorf("TotalAssets = %d, want 113280", bs.TotalAssets.Paise())
	}

	// Liabilities (normal Credit, balance = Cr - Dr):
	//   gst-output-payable  27000 - 9000 = 18000
	// TotalLiabilities = 18000.
	wantLiab := map[string]int64{
		"liabilities/gst-output-payable": 18000,
	}
	assertBalances(t, "liabilities", bs.Liabilities, wantLiab)
	if bs.TotalLiabilities.Paise() != 18000 {
		t.Errorf("TotalLiabilities = %d, want 18000", bs.TotalLiabilities.Paise())
	}
}

// TestIncomeStatementExact asserts income/expense normal-side balances, totals,
// and net income.
func TestIncomeStatementExact(t *testing.T) {
	lg := buildScenario(t)
	is := lg.IncomeStatement()

	// Income (normal Credit, balance = Cr - Dr):
	//   product-sales  150000 - 0     = 150000
	//   sales-returns  0      - 50000 = -50000  (contra-revenue, abnormal sign)
	// TotalIncome = 150000 + (-50000) = 100000.
	wantIncome := map[string]int64{
		"income/product-sales": 150000,
		"income/sales-returns": -50000,
	}
	assertBalances(t, "income", is.Income, wantIncome)
	if is.TotalIncome.Paise() != 100000 {
		t.Errorf("TotalIncome = %d, want 100000", is.TotalIncome.Paise())
	}

	// Expense (normal Debit, balance = Dr - Cr):
	//   gst-input       720
	//   processor-fees  4000
	// TotalExpense = 4720.
	wantExpense := map[string]int64{
		"expense/gst-input":      720,
		"expense/processor-fees": 4000,
	}
	assertBalances(t, "expense", is.Expense, wantExpense)
	if is.TotalExpense.Paise() != 4720 {
		t.Errorf("TotalExpense = %d, want 4720", is.TotalExpense.Paise())
	}

	// NetIncome = 100000 - 4720 = 95280.
	if is.NetIncome.Paise() != 95280 {
		t.Errorf("NetIncome = %d, want 95280", is.NetIncome.Paise())
	}
}

// TestAccountingIdentity asserts the balance sheet and income statement tie out:
// in v1 (no equity/retained-earnings node yet) Assets − Liabilities == NetIncome
// for a period closed from a zero opening balance, since every entry balances
// and the receivable clears to zero in this scenario.
//
//	A − L     = 113280 − 18000 = 95280
//	NetIncome = 100000 − 4720  = 95280
func TestAccountingIdentity(t *testing.T) {
	lg := buildScenario(t)
	bs := lg.BalanceSheet()
	is := lg.IncomeStatement()

	lhs := bs.TotalAssets.Sub(bs.TotalLiabilities)
	if lhs != is.NetIncome {
		t.Fatalf("Assets − Liabilities (%s) != NetIncome (%s)", lhs, is.NetIncome)
	}
	if is.NetIncome.Paise() != 95280 {
		t.Fatalf("NetIncome = %d, want 95280", is.NetIncome.Paise())
	}
}

// TestJournalOrder asserts the journal returns entries in posting order with
// their lines intact.
func TestJournalOrder(t *testing.T) {
	lg := buildScenario(t)
	j := lg.Journal()

	wantIKs := []string{"sale-1", "sale-2", "settle-1", "refund-1"}
	if len(j.Entries) != len(wantIKs) {
		t.Fatalf("journal has %d entries, want %d", len(j.Entries), len(wantIKs))
	}
	for i, ik := range wantIKs {
		if j.Entries[i].IK != ik {
			t.Errorf("journal[%d].IK = %q, want %q", i, j.Entries[i].IK, ik)
		}
	}
	// First entry's lines must match what was posted (3 lines, balanced).
	first := j.Entries[0]
	if len(first.Lines) != 3 {
		t.Fatalf("first entry has %d lines, want 3", len(first.Lines))
	}
	d, c := first.sumBySide()
	if d != c || d.Paise() != 118000 {
		t.Errorf("first entry sums: Dr=%s Cr=%s, want both 118000", d, c)
	}
}

// TestEmptyLedgerReports asserts reports on an empty ledger are well-formed
// zeros (pure function, no panics).
func TestEmptyLedgerReports(t *testing.T) {
	lg := New(newFakeChart())
	tb := lg.TrialBalance()
	if len(tb.Rows) != 0 || !tb.IsBalanced() || tb.TotalDr != 0 || tb.TotalCr != 0 {
		t.Errorf("empty trial balance not zero: %+v", tb)
	}
	is := lg.IncomeStatement()
	if is.NetIncome != 0 || is.TotalIncome != 0 || is.TotalExpense != 0 {
		t.Errorf("empty income statement not zero: %+v", is)
	}
	bs := lg.BalanceSheet()
	if bs.TotalAssets != 0 || bs.TotalLiabilities != 0 {
		t.Errorf("empty balance sheet not zero: %+v", bs)
	}
	if len(lg.Journal().Entries) != 0 {
		t.Errorf("empty journal not empty")
	}
}

// assertBalances checks a report section's accounts against expected normal-side
// balances (paise), independent of order.
func assertBalances(t *testing.T, section string, rows []AccountBalance, want map[string]int64) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("%s section has %d rows, want %d: %+v", section, len(rows), len(want), rows)
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.Account] = r.Balance.Paise()
	}
	for acct, w := range want {
		g, ok := got[acct]
		if !ok {
			t.Errorf("%s: missing account %q", section, acct)
			continue
		}
		if g != w {
			t.Errorf("%s: account %q balance = %d, want %d", section, acct, g, w)
		}
	}
}

// buildTimedScenario posts three balanced entries stamped at distinct, fixed
// timestamps so the period filter can be asserted exactly. No wall clock is used
// — the timestamps are constants, preserving determinism.
//
//	tMay (ts=100): dtc_sale gross 118000 = net 100000 + gst 18000
//	tJun (ts=200): dtc_sale gross  59000 = net  50000 + gst  9000
//	tJul (ts=300): refund_reversal of the June sale: net 50000 + gst 9000
//	               (Dr sales-returns 50000 ; Dr gst 9000 ; Cr receivable 59000)
//
// All three balance, so every period slice's trial balance also balances.
func buildTimedScenario(t *testing.T) *Ledger {
	t.Helper()
	lg := New(newFakeChart())
	post := func(e Entry) {
		t.Helper()
		if _, err := lg.Post(e); err != nil {
			t.Fatalf("Post %q: %v", e.IK, err)
		}
	}
	post(Entry{Type: "dtc_sale", IK: "may-sale", TxID: "1", Ts: 100, Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 118000),
		cr("income/product-sales", 100000),
		cr("liabilities/gst-output-payable", 18000),
	}})
	post(Entry{Type: "dtc_sale", IK: "jun-sale", TxID: "2", Ts: 200, Lines: []Line{
		dr("assets/razorpay-settlement-receivable", 59000),
		cr("income/product-sales", 50000),
		cr("liabilities/gst-output-payable", 9000),
	}})
	post(Entry{Type: "refund_reversal", IK: "jul-refund", TxID: "3", Ts: 300, Lines: []Line{
		dr("income/sales-returns", 50000),
		dr("liabilities/gst-output-payable", 9000),
		cr("assets/razorpay-settlement-receivable", 59000),
	}})
	return lg
}

// TestPeriodFilter is the table-driven assertion that the optional as-of/period
// filter selects whole entries by Ts and that every slice's trial balance still
// balances (ΣDr==ΣCr). It asserts exact net income, journal IKs, and the trial
// balance total for each window, including the report-over-all default.
func TestPeriodFilter(t *testing.T) {
	lg := buildTimedScenario(t)

	tests := []struct {
		name        string
		period      *Period // nil => call with no period (report over all)
		wantNet     int64   // expected NetIncome (paise)
		wantTotalDr int64   // expected trial-balance ΣDr (paise)
		wantIKs     []string
	}{
		{
			name:    "report over all (no period)",
			period:  nil,
			wantNet: 100000, // (100000+50000) income - 50000 returns - 0 expense
			// ΣDr = receivable Dr 177000 + gst Dr 9000 + sales-returns Dr 50000.
			wantTotalDr: 236000,
			wantIKs:     []string{"may-sale", "jun-sale", "jul-refund"},
		},
		{
			name:        "zero period equals report over all",
			period:      &Period{},
			wantNet:     100000,
			wantTotalDr: 236000,
			wantIKs:     []string{"may-sale", "jun-sale", "jul-refund"},
		},
		{
			name:        "as-of just after May only",
			period:      &Period{To: 200}, // ts < 200 => only ts=100
			wantNet:     100000,           // just the May sale's product-sales
			wantTotalDr: 118000,
			wantIKs:     []string{"may-sale"},
		},
		{
			name:        "June only (half-open [200,300))",
			period:      &Period{From: 200, To: 300},
			wantNet:     50000, // June sale only; refund at 300 excluded (exclusive To)
			wantTotalDr: 59000,
			wantIKs:     []string{"jun-sale"},
		},
		{
			name:        "from June onward (unbounded above)",
			period:      &Period{From: 200},
			wantNet:     0,      // June sale +50000 income, July refund -50000 returns
			wantTotalDr: 118000, // 59000 (jun) + 50000+9000 (jul refund Drs)
			wantIKs:     []string{"jun-sale", "jul-refund"},
		},
		{
			name:        "empty window selects nothing",
			period:      &Period{From: 1000, To: 2000},
			wantNet:     0,
			wantTotalDr: 0,
			wantIKs:     []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				tb TrialBalance
				is IncomeStatement
				j  Journal
			)
			if tc.period == nil {
				tb, is, j = lg.TrialBalance(), lg.IncomeStatement(), lg.Journal()
			} else {
				tb, is, j = lg.TrialBalance(*tc.period), lg.IncomeStatement(*tc.period), lg.Journal(*tc.period)
			}

			if !tb.IsBalanced() {
				t.Errorf("trial balance not balanced: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
			}
			if tb.TotalDr.Paise() != tc.wantTotalDr {
				t.Errorf("ΣDr = %d, want %d", tb.TotalDr.Paise(), tc.wantTotalDr)
			}
			if tb.TotalCr.Paise() != tc.wantTotalDr {
				t.Errorf("ΣCr = %d, want %d (must equal ΣDr)", tb.TotalCr.Paise(), tc.wantTotalDr)
			}
			if is.NetIncome.Paise() != tc.wantNet {
				t.Errorf("NetIncome = %d, want %d", is.NetIncome.Paise(), tc.wantNet)
			}
			gotIKs := make([]string, len(j.Entries))
			for i, e := range j.Entries {
				gotIKs[i] = e.IK
			}
			if len(gotIKs) != len(tc.wantIKs) {
				t.Fatalf("journal IKs = %v, want %v", gotIKs, tc.wantIKs)
			}
			for i := range gotIKs {
				if gotIKs[i] != tc.wantIKs[i] {
					t.Fatalf("journal IKs = %v, want %v", gotIKs, tc.wantIKs)
				}
			}
		})
	}
}

// TestPeriodFilterIsPure asserts the period-scoped reports do not mutate any
// hidden state: calling a narrow window and then report-over-all yields the same
// full report as calling report-over-all first.
func TestPeriodFilterIsPure(t *testing.T) {
	lg := buildTimedScenario(t)
	allFirst := lg.TrialBalance()
	_ = lg.TrialBalance(Period{From: 200, To: 300}) // a narrow query in between
	allAgain := lg.TrialBalance()
	if allFirst.TotalDr != allAgain.TotalDr || allFirst.TotalCr != allAgain.TotalCr ||
		len(allFirst.Rows) != len(allAgain.Rows) {
		t.Fatalf("report-over-all changed after a scoped query: before %+v after %+v", allFirst, allAgain)
	}
}

// TestPeriodContains unit-tests the half-open [From,To) membership rule and the
// unbounded-on-zero-bound semantics directly.
func TestPeriodContains(t *testing.T) {
	tests := []struct {
		name   string
		period Period
		ts     int64
		want   bool
	}{
		{"unbounded includes zero ts", Period{}, 0, true},
		{"unbounded includes any ts", Period{}, 999, true},
		{"From inclusive", Period{From: 100}, 100, true},
		{"below From excluded", Period{From: 100}, 99, false},
		{"To exclusive", Period{To: 200}, 200, false},
		{"just below To included", Period{To: 200}, 199, true},
		{"inside half-open window", Period{From: 100, To: 200}, 150, true},
		{"at exclusive upper edge", Period{From: 100, To: 200}, 200, false},
		{"unstamped excluded by lower bound", Period{From: 1}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.period.contains(tc.ts); got != tc.want {
				t.Errorf("Period%+v.contains(%d) = %v, want %v", tc.period, tc.ts, got, tc.want)
			}
		})
	}
}

// TestReportPanicsOnMultiplePeriods asserts the variadic guard: passing more
// than one Period is a programming error and panics rather than silently
// ignoring extras.
func TestReportPanicsOnMultiplePeriods(t *testing.T) {
	lg := buildTimedScenario(t)
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic when passing two periods")
		}
	}()
	_ = lg.TrialBalance(Period{}, Period{From: 100})
}

// TestNormalNet directly verifies the sign convention helper for both normal
// sides, including abnormal (contra) results.
func TestNormalNet(t *testing.T) {
	tests := []struct {
		normal Side
		dr, cr int64
		want   int64
	}{
		{Debit, 100, 30, 70},   // normal-Dr, debit-heavy
		{Debit, 30, 100, -70},  // normal-Dr, abnormal credit balance
		{Credit, 100, 30, -70}, // normal-Cr, abnormal debit balance
		{Credit, 30, 100, 70},  // normal-Cr, credit-heavy
		{Debit, 0, 0, 0},       // empty
	}
	for _, tc := range tests {
		got := normalNet(tc.normal, money.FromPaise(tc.dr), money.FromPaise(tc.cr))
		if got.Paise() != tc.want {
			t.Errorf("normalNet(%s, %d, %d) = %d, want %d", tc.normal, tc.dr, tc.cr, got.Paise(), tc.want)
		}
	}
}
