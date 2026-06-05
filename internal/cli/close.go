package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newCloseCmd: `close-agent close --world <string> --period <YYYY-MM>`
// Will run the full close workflow (ingest → normalize → classify → post →
// reconcile → reports → score) and print the score.
func newCloseCmd(out io.Writer) *cobra.Command {
	var world, period string
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Run the close workflow for a period and print the score",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notImplemented(out, fmt.Sprintf("close --world %s --period %s", world, period))
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	return cmd
}
