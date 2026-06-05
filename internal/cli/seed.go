package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newSeedCmd: `close-agent seed --world <string> --period <YYYY-MM>`
// Will generate the substrate (razorpay fixtures + bank-feed + truth GL).
func newSeedCmd(out io.Writer) *cobra.Command {
	var world, period string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Generate a deterministic fixture period (substrate + bank-feed + truth)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notImplemented(out, fmt.Sprintf("seed --world %s --period %s", world, period))
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	return cmd
}
