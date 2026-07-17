package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/razorpay/ledger-flow/internal/score"
	"github.com/spf13/cobra"
)

// newDiffCmd: `ledger-flow diff --world <string> --period <YYYY-MM>` (SPEC §10).
//
// It runs the deterministic close and prints the diff of the produced ledger
// against the hidden truth GL, line by line: every wrong/missing/extra entry
// (got vs want) and every non-zero per-account balance delta. On the clean
// dtc/2026-05 period it reports "no differences"; on a tampered period it lists
// the exact differences. The comparison happens behind the scorer boundary (only
// the scorer reads truth, SPEC §4.4) — diff renders the score.RunRecord the close
// already built.
func newDiffCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff the produced ledger against ground truth, line by line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("diff: resolve working directory: %w", err)
				}
				root = wd
			}
			res, err := run.Run(root, world, period)
			if err != nil {
				return err
			}
			renderDiff(out, world, period, res.Record)
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// renderDiff prints the entry-level and account-level differences in the frozen
// RunRecord. A clean run (no error records, all deltas zero) prints "no
// differences vs truth", which is the gate's clean-period expectation.
func renderDiff(out io.Writer, world, period string, rec score.RunRecord) {
	fmt.Fprintf(out, "DIFF vs truth — world %q period %q\n", world, period)

	nonZero := nonZeroDeltas(rec.PerAccountDeltas)
	if len(rec.Errors) == 0 && len(nonZero) == 0 {
		fmt.Fprintf(out, "  no differences vs truth (score %d%%)\n", rec.ScorePct)
		return
	}

	if len(rec.Errors) > 0 {
		fmt.Fprintf(out, "  entry differences: %d (wrong=%d missing=%d extra=%d)\n",
			len(rec.Errors), rec.Totals.Wrong, rec.Totals.Missing, rec.Totals.Extra)
		for _, e := range rec.Errors {
			fmt.Fprintf(out, "    - [%s] %s\n", e.Class, e.EventID)
			if e.Got != "" {
				fmt.Fprintf(out, "        got:  %s\n", e.Got)
			}
			if e.Want != "" {
				fmt.Fprintf(out, "        want: %s\n", e.Want)
			}
		}
	}

	if len(nonZero) > 0 {
		fmt.Fprintf(out, "  account balance deltas: %d\n", len(nonZero))
		for _, d := range nonZero {
			fmt.Fprintf(out, "    - %-40s got=%s want=%s delta=%s\n",
				d.Account, d.GotBalance.String(), d.WantBalance.String(), d.Delta.String())
		}
	}

	fmt.Fprintf(out, "  score = %d%%\n", rec.ScorePct)
}

// nonZeroDeltas keeps only the per-account deltas that actually differ, so a
// clean account (delta 0) does not clutter the diff. Input order (account path)
// is preserved.
func nonZeroDeltas(deltas []score.AccountDelta) []score.AccountDelta {
	out := make([]score.AccountDelta, 0, len(deltas))
	for _, d := range deltas {
		if !d.Delta.IsZero() {
			out = append(out, d)
		}
	}
	return out
}
