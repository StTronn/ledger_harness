package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newDiffCmd: `close-agent diff --world <string> --period <YYYY-MM>`
// Will diff the produced ledger against truth/gl.json line by line.
func newDiffCmd(out io.Writer) *cobra.Command {
	var world, period string
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff the produced ledger against ground truth, line by line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notImplemented(out, fmt.Sprintf("diff --world %s --period %s", world, period))
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	return cmd
}
