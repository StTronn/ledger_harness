package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/seed"
	"github.com/spf13/cobra"
)

// newSeedCmd: `close-agent seed --world <string> --period <YYYY-MM>` (SPEC §10).
// It deterministically generates the substrate for one period — Razorpay-shaped
// fixtures + an independent bank feed + the hidden truth GL — under
// worlds/<world>/<period>/ (SPEC §2 Phase 2, §4.4). Re-running with the same
// flags overwrites with byte-identical content.
func newSeedCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Generate a deterministic fixture period (substrate + bank-feed + truth)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The seeder writes under <root>/worlds/...; the repo root defaults to
			// the process working directory (run from the module root). The hidden
			// --root flag lets tests redirect the write into a temp dir without
			// touching the SPEC §10 flag surface (--world/--period).
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("seed: resolve working directory: %w", err)
				}
				root = wd
			}
			res, err := seed.Seed(root, world, period)
			if err != nil {
				return err
			}
			printSeedResult(out, res)
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// printSeedResult prints a concise summary of what the seed run wrote, so the
// operator can see the substrate was produced and where it landed.
func printSeedResult(out io.Writer, res seed.Result) {
	l := res.Layout
	fmt.Fprintf(out, "seeded world %q period %q under %s\n", l.World, l.Period, l.PeriodDir())
	fmt.Fprintf(out, "  razorpay/payments.json    %d payments\n", res.NumPayments)
	fmt.Fprintf(out, "  razorpay/refunds.json     %d refunds\n", res.NumRefunds)
	fmt.Fprintf(out, "  razorpay/settlements.json %d settlements\n", res.NumSettlements)
	fmt.Fprintf(out, "  razorpay/disputes.json    %d disputes\n", res.NumDisputes)
	fmt.Fprintf(out, "  bank-feed.json            %d credits, %d debits\n", res.BankCredits, res.BankDebits)
	fmt.Fprintf(out, "  truth/gl.json             %d entries (scorer only)\n", res.NumGLEntries)
}
