package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newShowCmd: `close-agent show <playbook|trace>`. A parent command with two
// sub-subcommands per SPEC §10:
//
//	show playbook        print the entry types (the schema file)
//	show trace <path>    print an agent trajectory at the given run path
func newShowCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Inspect engine artifacts (the playbook or an agent trace)",
	}
	cmd.AddCommand(newShowPlaybookCmd(out), newShowTraceCmd(out))
	return cmd
}

// newShowPlaybookCmd: `close-agent show playbook` — will print the chart of
// accounts and entry types from the playbook schema file.
func newShowPlaybookCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "playbook",
		Short: "Print the chart of accounts and entry types",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notImplemented(out, "show playbook")
			return nil
		},
	}
}

// newShowTraceCmd: `close-agent show trace <path>` — will print the recorded
// agent trajectory found under the given run path.
func newShowTraceCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "trace <path>",
		Short: "Print an agent trajectory from a run path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			notImplemented(out, fmt.Sprintf("show trace %s", args[0]))
			return nil
		},
	}
}
