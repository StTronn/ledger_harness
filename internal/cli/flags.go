package cli

import "github.com/spf13/cobra"

// addWorldPeriodFlags registers the --world / --period pair shared by seed,
// close, report, and diff. Flag names match SPEC §10 exactly. The period is a
// YYYY-MM string (validation is added in a later phase, not Phase 0).
func addWorldPeriodFlags(cmd *cobra.Command, world, period *string) {
	cmd.Flags().StringVar(world, "world", "", "world to operate on (e.g. dtc)")
	cmd.Flags().StringVar(period, "period", "", "accounting period as YYYY-MM (e.g. 2026-05)")
}
