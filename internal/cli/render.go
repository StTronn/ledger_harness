package cli

import (
	"fmt"
	"io"

	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/razorpay/close-agent/internal/money"
)

// render.go holds the terminal renderers for the four SPEC §10 reports. Each is a
// pure projection of a ledger report value (itself a pure function of posted
// entries, SPEC §6) onto aligned text. Amounts are right-aligned rupee strings
// (money.Money.String — integer paise formatting, no float, SPEC §1). The
// trial-balance and balance-sheet renderers print and ASSERT the balancing
// identity, so a report that did not balance is visible (and would fail the gate).

// amtWidth is the column width for a right-aligned rupee amount. It comfortably
// fits a period's largest account total in the DTC world.
const amtWidth = 16

// header writes the report banner naming the report, world, and period.
func header(out io.Writer, title, world, period string) error {
	_, err := fmt.Fprintf(out, "%s — world %q period %q\n", title, world, period)
	return err
}

// renderTrialBalance prints one row per account (Dr and Cr columns) plus the
// column totals, then asserts ΣDr == ΣCr — the defining property of the trial
// balance (SPEC §6, §10). A non-balancing trial balance returns an error so the
// gate catches it; for a ledger built only through Post() it always balances.
func renderTrialBalance(out io.Writer, world, period string, tb ledger.TrialBalance) error {
	if err := header(out, "TRIAL BALANCE", world, period); err != nil {
		return err
	}
	fmt.Fprintf(out, "  %-40s %*s %*s\n", "ACCOUNT", amtWidth, "DEBIT", amtWidth, "CREDIT")
	for _, r := range tb.Rows {
		fmt.Fprintf(out, "  %-40s %*s %*s\n", r.Account, amtWidth, r.Debit.String(), amtWidth, r.Credit.String())
	}
	fmt.Fprintf(out, "  %-40s %*s %*s\n", "TOTAL", amtWidth, tb.TotalDr.String(), amtWidth, tb.TotalCr.String())
	if !tb.IsBalanced() {
		return fmt.Errorf("report: trial balance does not balance: ΣDr=%s ΣCr=%s", tb.TotalDr, tb.TotalCr)
	}
	fmt.Fprintf(out, "  balanced: ΣDr == ΣCr (%s)\n", tb.TotalDr.String())
	return nil
}

// renderBalanceSheet prints the assets and liabilities sections (normal-side
// balances) with section totals (SPEC §6, §10).
func renderBalanceSheet(out io.Writer, world, period string, bs ledger.BalanceSheet) error {
	if err := header(out, "BALANCE SHEET", world, period); err != nil {
		return err
	}
	fmt.Fprintf(out, "  ASSETS\n")
	for _, a := range bs.Assets {
		fmt.Fprintf(out, "    %-38s %*s\n", a.Account, amtWidth, a.Balance.String())
	}
	fmt.Fprintf(out, "    %-38s %*s\n", "TOTAL ASSETS", amtWidth, bs.TotalAssets.String())
	fmt.Fprintf(out, "  LIABILITIES\n")
	for _, l := range bs.Liabilities {
		fmt.Fprintf(out, "    %-38s %*s\n", l.Account, amtWidth, l.Balance.String())
	}
	fmt.Fprintf(out, "    %-38s %*s\n", "TOTAL LIABILITIES", amtWidth, bs.TotalLiabilities.String())
	// In v1 income/expense are not yet closed to equity, so Assets − Liabilities
	// equals net income for the period (SPEC §4.1 identity). Print it as the
	// retained position so the statement is self-checking.
	net := bs.TotalAssets.Sub(bs.TotalLiabilities)
	fmt.Fprintf(out, "  ASSETS − LIABILITIES = %s (period net income)\n", net.String())
	return nil
}

// renderIncome prints the income and expense sections (normal-side balances)
// with section totals and the net-income bottom line (SPEC §6, §10).
func renderIncome(out io.Writer, world, period string, is ledger.IncomeStatement) error {
	if err := header(out, "INCOME STATEMENT", world, period); err != nil {
		return err
	}
	fmt.Fprintf(out, "  INCOME\n")
	for _, a := range is.Income {
		fmt.Fprintf(out, "    %-38s %*s\n", a.Account, amtWidth, a.Balance.String())
	}
	fmt.Fprintf(out, "    %-38s %*s\n", "TOTAL INCOME", amtWidth, is.TotalIncome.String())
	fmt.Fprintf(out, "  EXPENSE\n")
	for _, a := range is.Expense {
		fmt.Fprintf(out, "    %-38s %*s\n", a.Account, amtWidth, a.Balance.String())
	}
	fmt.Fprintf(out, "    %-38s %*s\n", "TOTAL EXPENSE", amtWidth, is.TotalExpense.String())
	fmt.Fprintf(out, "  %-40s %*s\n", "NET INCOME", amtWidth, is.NetIncome.String())
	return nil
}

// renderJournal prints the posted entries in posting order (the audit trail,
// SPEC §10): each entry's type, idempotency key, and balanced lines. Per-entry it
// asserts ΣDr == ΣCr; a non-balancing entry could never have been posted, but the
// assertion makes the journal self-checking.
func renderJournal(out io.Writer, world, period string, j ledger.Journal) error {
	if err := header(out, "JOURNAL", world, period); err != nil {
		return err
	}
	fmt.Fprintf(out, "  %d entries\n", len(j.Entries))
	for _, e := range j.Entries {
		fmt.Fprintf(out, "  [%s] %s tx=%s\n", e.Type, e.IK, e.TxID)
		var dr, cr money.Money
		for _, l := range e.Lines {
			fmt.Fprintf(out, "    %-2s %-40s %*s\n", l.Side, l.Account, amtWidth, l.Amount.String())
			switch l.Side {
			case ledger.Debit:
				dr = dr.Add(l.Amount)
			case ledger.Credit:
				cr = cr.Add(l.Amount)
			}
		}
		if dr != cr {
			return fmt.Errorf("report: journal entry %q does not balance: ΣDr=%s ΣCr=%s", e.IK, dr, cr)
		}
	}
	return nil
}
