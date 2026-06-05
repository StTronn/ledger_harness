package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/closer"
	"github.com/spf13/cobra"
)

// newCloseCmd: `close-agent close --world <string> --period <YYYY-MM>` (SPEC §10).
// It runs the deterministic close pipeline end to end (ingest → normalize →
// classify → bind+post) over the period's fixtures and scores the produced ledger
// against the hidden truth GL, printing the score plus the classified/skipped
// counts (SPEC §2 Phase 4, §5, §9).
//
// In Phase 4 there is NO agent: events the rule engine cannot classify are
// FLAGGED and SKIPPED (reported, not crashed). On the clean dtc/2026-05 period
// every event classifies and the score is the 100% deterministic baseline.
func newCloseCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Run the close workflow for a period and print the score",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// worlds/ live under <root>; default to the working directory (the
			// module root in normal use). The hidden --root flag lets tests point at
			// a temp-seeded period without touching the SPEC §10 flag surface.
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("close: resolve working directory: %w", err)
				}
				root = wd
			}
			res, err := closer.Run(root, world, period)
			if err != nil {
				return err
			}
			printCloseResult(out, world, period, res)
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// printCloseResult prints a concise close summary: the classified/skipped counts,
// any skipped events (flagged, not crashed), and the score against truth. The
// score line ("score = N%") is the deterministic baseline the gate checks.
func printCloseResult(out io.Writer, world, period string, res closer.Result) {
	fmt.Fprintf(out, "close world %q period %q\n", world, period)
	fmt.Fprintf(out, "  classified: %d events -> %d posted entries\n", res.Classified, res.Ledger.Len())
	fmt.Fprintf(out, "  skipped:    %d events\n", len(res.Skipped))
	for _, s := range res.Skipped {
		fmt.Fprintf(out, "    - %s %s: %s\n", s.Type, s.EventID, s.Reason)
	}

	sc := res.Score
	tb := "no"
	if sc.TrialBalanceMatches {
		tb = "yes"
	}
	// Reconciliation (SPEC §7): list the breaks the three checks detected. On the
	// clean period this is "0 breaks (reconciled)". In Phase 5 no agent resolves
	// them; each break carries the context a Phase-8 investigate agent will need.
	if len(res.Breaks) == 0 {
		fmt.Fprintf(out, "  reconcile: 0 breaks (reconciled)\n")
	} else {
		fmt.Fprintf(out, "  reconcile: %d break(s)\n", len(res.Breaks))
		for _, b := range res.Breaks {
			where := b.SettlementID
			if where == "" {
				where = "(period)"
			}
			fmt.Fprintf(out, "    - check#%d %s [%s] expected=%s actual=%s candidates=%v\n",
				b.Check, b.Kind, where, b.Expected, b.Actual, b.CandidateEventIDs)
			fmt.Fprintf(out, "        %s\n", b.Detail)
		}
	}

	fmt.Fprintf(out, "  trial balance matches truth: %s\n", tb)
	fmt.Fprintf(out, "  entries correct: %d/%d\n", sc.Correct, sc.Total)
	if len(sc.Errors) > 0 {
		fmt.Fprintf(out, "  scoring errors: %d\n", len(sc.Errors))
		for _, e := range sc.Errors {
			fmt.Fprintf(out, "    - [%s] %s\n", e.Class, e.EventID)
		}
	}
	// Report the frozen errors.json artifact path (SPEC §9, §10) so the operator
	// knows the single learning seam was written for this run.
	if res.ErrorsPath != "" {
		fmt.Fprintf(out, "  errors record: %s (schema v%d)\n", res.ErrorsPath, res.Record.SchemaVersion)
	}
	fmt.Fprintf(out, "score = %d%%\n", sc.Percent())
}
