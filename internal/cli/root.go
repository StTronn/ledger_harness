// Package cli wires the close-agent cobra command tree. It is kept separate
// from package main so the command surface is unit-testable: NewRootCmd builds
// a fresh tree and Execute drives it, both without touching os.Args or os.Exit.
//
// Phase 0 deliberately ships stubs only. Every leaf command parses its flags
// and prints a clear "not implemented yet" message; none returns an error or
// crashes. Business logic lands in later phases behind these same flag surfaces,
// which match SPEC §10 exactly.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// defaultPlaybookPath is where the playbook (chart of accounts + entry types)
// lives relative to the repo root. Commands that will eventually load schema
// reference this so the path is defined in one place.
const defaultPlaybookPath = "config/playbook.json"

// NewRootCmd builds the full command tree rooted at "close-agent" with output
// routed to out. Passing the writer in keeps the tree free of global state and
// lets tests capture output deterministically.
func NewRootCmd(out io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "close-agent",
		Short: "Close a month of Razorpay activity into balanced, reconciled books",
		Long: "close-agent ingests one period of a DTC brand's Razorpay activity, " +
			"produces a balanced double-entry ledger and financial reports, " +
			"reconciles to the bank feed, and scores the result against ground truth.",
		// Silence cobra's own error/usage printing; main.go owns exit codes and
		// the leaf stubs never return errors in Phase 0.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(out)

	root.AddCommand(
		newSeedCmd(out),
		newCloseCmd(out),
		newReportCmd(out),
		newShowCmd(out),
		newDiffCmd(out),
		newRecordResponsesCmd(out),
		newRecordInvestigationsCmd(out),
		newClassifyCmd(out),
		newInvestigateCmd(out),
	)
	return root
}

// Execute builds the root command, runs it against the given args, and returns
// any error to the caller (main decides the process exit code). Output is
// written to out.
func Execute(args []string, out io.Writer) error {
	root := NewRootCmd(out)
	root.SetArgs(args)
	return root.Execute()
}

// notImplemented prints a uniform, non-error stub message for a command. It is
// the single place the Phase 0 "stub" behavior is defined.
func notImplemented(out io.Writer, command string) {
	fmt.Fprintf(out, "close-agent %s: not implemented yet (phase 0 scaffolding)\n", command)
}
