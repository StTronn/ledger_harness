package ledger

import (
	"sort"

	"github.com/razorpay/close-agent/internal/money"
)

// The four report queries (SPEC §6, §10) are PURE functions of the posted
// entries plus the chart's structure. They hold no state: each call re-derives
// balances by folding over Entries() in posting order, so the same ledger always
// yields the same reports. Balances follow the one sign convention documented in
// ledger.go — a normal-Debit account's balance is ΣDr−ΣCr, a normal-Credit
// account's is ΣCr−ΣDr.

// Period is an OPTIONAL as-of / period filter over entries by their Ts (Unix
// seconds, SPEC §4.3, §6 "?at=" / "?period="). It is a half-open interval
// [From, To): an entry is included iff From <= Ts < To. A zero From means "no
// lower bound" (since the start of time) and a zero To means "no upper bound"
// (through the end of time), so the zero Period{} includes every entry — which
// is the report-over-all default used when no period is supplied. An entry with
// Ts == 0 (unstamped) is therefore included only by an unbounded report.
//
// Half-open is deliberate: an "as-of T" balance-sheet/trial-balance query is
// Period{To: T} and a "period [A, B)" income-statement query is
// Period{From: A, To: B}, so adjacent periods partition entries without
// double-counting the boundary instant.
type Period struct {
	From int64 // inclusive lower bound on Ts; 0 = unbounded below
	To   int64 // exclusive upper bound on Ts; 0 = unbounded above
}

// contains reports whether ts falls in the period under the half-open [From,To)
// rule, treating a zero bound as unbounded on that side.
func (p Period) contains(ts int64) bool {
	if p.From != 0 && ts < p.From {
		return false
	}
	if p.To != 0 && ts >= p.To {
		return false
	}
	return true
}

// effectivePeriod collapses the variadic period argument the public reports
// take into a single Period. Zero periods => the unbounded zero Period (report
// over all); exactly one => that period. More than one is a programming error
// and panics, because silently picking one would hide a caller bug.
func effectivePeriod(periods []Period) Period {
	switch len(periods) {
	case 0:
		return Period{}
	case 1:
		return periods[0]
	default:
		panic("ledger: at most one Period may be passed to a report")
	}
}

// AccountBalance is one account's net balance, reported on its normal side.
//
// Debit and Credit are the raw posted totals (always >= 0). NormalSide is the
// account's normal side. Balance is the signed net stated on the normal side:
// for a normal-Debit account Balance = Debit − Credit; for a normal-Credit
// account Balance = Credit − Debit. A positive Balance therefore always means
// "this account carries a normal balance of this magnitude"; a negative Balance
// means it is carrying an abnormal (contra) balance.
type AccountBalance struct {
	Account    string
	NormalSide Side
	Debit      money.Money
	Credit     money.Money
	Balance    money.Money
}

// balances folds every posted line whose entry falls in the period into
// per-account Dr/Cr totals. It is the shared core of all four reports: trial
// balance, balance sheet, and income statement are different views over this
// same fold, and the journal is the raw entries. Returns a map keyed by account
// path. The fold reads only lg.entries (posting order, the source of truth) and
// the period, so it stays a pure function of the posted entries.
func (lg *Ledger) balances(p Period) map[string]*AccountBalance {
	acc := make(map[string]*AccountBalance)
	for _, e := range lg.entries {
		if !p.contains(e.Ts) {
			continue
		}
		for _, l := range e.Lines {
			b := acc[l.Account]
			if b == nil {
				b = &AccountBalance{
					Account:    l.Account,
					NormalSide: lg.chart.NormalSide(l.Account),
				}
				acc[l.Account] = b
			}
			switch l.Side {
			case Debit:
				b.Debit = b.Debit.Add(l.Amount)
			case Credit:
				b.Credit = b.Credit.Add(l.Amount)
			}
		}
	}
	// Finalize each account's net balance on its normal side.
	for _, b := range acc {
		b.Balance = normalNet(b.NormalSide, b.Debit, b.Credit)
	}
	return acc
}

// normalNet returns the net balance stated on the account's normal side: the
// single place that turns raw Dr/Cr totals into a signed "balance". This is the
// sign convention from the package doc, applied once and reused everywhere.
func normalNet(normal Side, dr, cr money.Money) money.Money {
	if normal == Credit {
		return cr.Sub(dr)
	}
	// Default (including normal Debit) is Dr − Cr.
	return dr.Sub(cr)
}

// TrialBalance is the list of every account that has activity, each with its raw
// Dr and Cr totals, plus the column sums. Per the SPEC the trial balance must
// satisfy ΣDr == ΣCr (that is the whole point of double-entry); IsBalanced
// reports that, and it is always true for a ledger built only through Post()
// because every posted entry balances.
type TrialBalance struct {
	Rows    []TrialBalanceRow
	TotalDr money.Money
	TotalCr money.Money
}

// TrialBalanceRow is one account's debit and credit totals.
type TrialBalanceRow struct {
	Account string
	Debit   money.Money
	Credit  money.Money
}

// IsBalanced reports whether the debit and credit column totals are equal.
func (tb TrialBalance) IsBalanced() bool { return tb.TotalDr == tb.TotalCr }

// TrialBalance computes the trial balance: one row per account with activity,
// ordered by account path, with the Dr and Cr column totals. Accounts with no
// postings are omitted (they would be all-zero rows). ΣDr and ΣCr are the column
// sums; for a ledger built only through Post() they are always equal — and they
// stay equal under any period filter, because the filter selects whole entries
// (each of which balances), never individual lines.
//
// An optional Period restricts the report to entries whose Ts falls in it; with
// no Period it reports over all entries (the Phase 1 default).
func (lg *Ledger) TrialBalance(period ...Period) TrialBalance {
	bals := lg.balances(effectivePeriod(period))
	paths := sortedKeys(bals)

	tb := TrialBalance{Rows: make([]TrialBalanceRow, 0, len(paths))}
	for _, p := range paths {
		b := bals[p]
		tb.Rows = append(tb.Rows, TrialBalanceRow{
			Account: b.Account,
			Debit:   b.Debit,
			Credit:  b.Credit,
		})
		tb.TotalDr = tb.TotalDr.Add(b.Debit)
		tb.TotalCr = tb.TotalCr.Add(b.Credit)
	}
	return tb
}

// BalanceSheet is the assets / liabilities view at a point in time. Each section
// lists its accounts (with normal-side balances) and the section total. The
// accounting identity holds: TotalAssets − TotalLiabilities equals net income
// (the income-statement bottom line) for a period closed from zero, because
// every entry balances and income/expense have not yet been closed to equity in
// v1 (there is no retained-earnings account in the ~10-node chart). Reports stay
// pure: this is recomputed from entries each call.
type BalanceSheet struct {
	Assets           []AccountBalance
	Liabilities      []AccountBalance
	TotalAssets      money.Money
	TotalLiabilities money.Money
}

// BalanceSheet computes the balance sheet: assets and liabilities accounts with
// their normal-side balances, plus section totals. Accounts with a zero net
// balance are still listed if they had activity, so the statement shows cleared
// accounts (e.g. the receivable that nets to ~0). Accounts with no activity are
// omitted.
//
// An optional Period restricts the report to entries whose Ts falls in it; an
// "as-of T" balance sheet is Period{To: T}. With no Period it reports over all
// entries (the Phase 1 default).
func (lg *Ledger) BalanceSheet(period ...Period) BalanceSheet {
	bals := lg.balances(effectivePeriod(period))
	bs := BalanceSheet{}
	for _, p := range sortedKeys(bals) {
		b := *bals[p]
		switch rootOf(p) {
		case "assets":
			bs.Assets = append(bs.Assets, b)
			bs.TotalAssets = bs.TotalAssets.Add(b.Balance)
		case "liabilities":
			bs.Liabilities = append(bs.Liabilities, b)
			bs.TotalLiabilities = bs.TotalLiabilities.Add(b.Balance)
		}
	}
	return bs
}

// IncomeStatement is the income / expense view over a period. Income is listed
// on its normal (credit) side, expense on its normal (debit) side. NetIncome is
// TotalIncome − TotalExpense, the bottom line. Note sales-returns is income-root
// contra-revenue, so a refund reduces TotalIncome here — consistent with the
// chart (SPEC §4.1).
type IncomeStatement struct {
	Income       []AccountBalance
	Expense      []AccountBalance
	TotalIncome  money.Money
	TotalExpense money.Money
	NetIncome    money.Money
}

// IncomeStatement computes the income statement: income and expense accounts
// with their normal-side balances, section totals, and net income
// (TotalIncome − TotalExpense). Pure function of posted entries.
//
// An optional Period restricts the report to entries whose Ts falls in it; a
// "period [A, B)" income statement is Period{From: A, To: B}. With no Period it
// reports over all entries (the Phase 1 default).
func (lg *Ledger) IncomeStatement(period ...Period) IncomeStatement {
	bals := lg.balances(effectivePeriod(period))
	is := IncomeStatement{}
	for _, p := range sortedKeys(bals) {
		b := *bals[p]
		switch rootOf(p) {
		case "income":
			is.Income = append(is.Income, b)
			is.TotalIncome = is.TotalIncome.Add(b.Balance)
		case "expense":
			is.Expense = append(is.Expense, b)
			is.TotalExpense = is.TotalExpense.Add(b.Balance)
		}
	}
	is.NetIncome = is.TotalIncome.Sub(is.TotalExpense)
	return is
}

// Journal is the chronological list of posted entries — the audit trail. It is
// simply the entries in posting order (SPEC §10 journal export). Each entry's
// lines are returned as posted.
type Journal struct {
	Entries []Entry
}

// Journal returns the journal: posted entries in posting order, as defensive
// copies so the caller cannot mutate the ledger through it. An optional Period
// restricts the journal to entries whose Ts falls in it; with no Period it
// returns all entries (the Phase 1 default). Posting order is preserved within
// the filtered set.
func (lg *Ledger) Journal(period ...Period) Journal {
	p := effectivePeriod(period)
	all := lg.Entries()
	if p == (Period{}) {
		return Journal{Entries: all}
	}
	kept := make([]Entry, 0, len(all))
	for _, e := range all {
		if p.contains(e.Ts) {
			kept = append(kept, e)
		}
	}
	return Journal{Entries: kept}
}

// rootOf returns the first path segment of an account path (its root). For
// "income/product-sales" it returns "income". An empty or rootless path returns
// the path unchanged.
func rootOf(path string) string {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return path
}

// sortedKeys returns the keys of a balance map sorted lexically, so every report
// has a deterministic, stable row order independent of map iteration order.
func sortedKeys(m map[string]*AccountBalance) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
