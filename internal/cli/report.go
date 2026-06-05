package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// reportKinds enumerates the valid --kind values per SPEC §10. The set is fixed
// in v1; the rule engine and reports must agree on these names.
var reportKinds = []string{"trial-balance", "balance-sheet", "income", "journal"}

// newReportCmd: `close-agent report --world <string> --period <YYYY-MM>
// --kind <trial-balance|balance-sheet|income|journal>`
func newReportCmd(out io.Writer) *cobra.Command {
	var world, period, kind string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print a financial report for a period",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notImplemented(out, fmt.Sprintf("report --world %s --period %s --kind %s", world, period, kind))
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&kind, "kind", "",
		fmt.Sprintf("report kind: one of %v", reportKinds))
	return cmd
}
