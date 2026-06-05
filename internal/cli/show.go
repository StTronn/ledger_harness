package cli

import (
	"fmt"
	"io"

	"github.com/razorpay/close-agent/internal/config"
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

// newShowPlaybookCmd: `close-agent show playbook` — loads the playbook schema
// file (chart of accounts + entry types) and prints it. Loading runs the full
// load-time validation, so this command also proves the playbook is well-formed.
func newShowPlaybookCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playbook",
		Short: "Print the chart of accounts and entry types",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("playbook")
			if path == "" {
				path = defaultPlaybookPath
			}
			pb, err := config.Load(path)
			if err != nil {
				return err
			}
			return pb.Print(out)
		},
	}
	cmd.Flags().String("playbook", defaultPlaybookPath, "path to the playbook JSON file")
	return cmd
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
