package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/closer"
	"github.com/razorpay/close-agent/internal/ledger"
	"github.com/spf13/cobra"
)

// reportKinds enumerates the valid --kind values per SPEC §10. The set is fixed
// in v1; the rule engine and reports must agree on these names.
var reportKinds = []string{"trial-balance", "balance-sheet", "income", "journal"}

// newReportCmd: `close-agent report --world <string> --period <YYYY-MM>
// --kind <trial-balance|balance-sheet|income|journal>` (SPEC §10).
//
// It runs the deterministic close pipeline to build the posted ledger for the
// period, then renders the requested report. The reports are PURE functions of
// the posted entries (SPEC §6), so each render is reproducible and the
// trial-balance / balance-sheet totals balance by construction.
func newReportCmd(out io.Writer) *cobra.Command {
	var world, period, kind, root string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print a financial report for a period",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !validReportKind(kind) {
				return fmt.Errorf("report: --kind must be one of %v (got %q)", reportKinds, kind)
			}
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("report: resolve working directory: %w", err)
				}
				root = wd
			}
			res, err := closer.Run(root, world, period)
			if err != nil {
				return err
			}
			return renderReport(out, kind, world, period, res.Ledger)
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&kind, "kind", "",
		fmt.Sprintf("report kind: one of %v", reportKinds))
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// validReportKind reports whether k is one of the SPEC §10 report kinds.
func validReportKind(k string) bool {
	for _, rk := range reportKinds {
		if k == rk {
			return true
		}
	}
	return false
}

// renderReport dispatches on kind and prints the corresponding report from the
// posted ledger. Each renderer is a pure read of the ledger (no period filter:
// the close already scopes a single period's fixtures, SPEC §6 default).
func renderReport(out io.Writer, kind, world, period string, lg *ledger.Ledger) error {
	switch kind {
	case "trial-balance":
		return renderTrialBalance(out, world, period, lg.TrialBalance())
	case "balance-sheet":
		return renderBalanceSheet(out, world, period, lg.BalanceSheet())
	case "income":
		return renderIncome(out, world, period, lg.IncomeStatement())
	case "journal":
		return renderJournal(out, world, period, lg.Journal())
	default:
		return fmt.Errorf("report: unknown kind %q", kind)
	}
}
