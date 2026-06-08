package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/spf13/cobra"
)

// classify.go is the CLI for the ASYNCHRONOUS classify pipeline (the redefined §8
// classify seam). The deterministic close PARKS its long tail: `close --agent off`
// books what the rules can and emits proposals.json (its skipped events) — the work
// queue. The two commands here process that queue out of band:
//
//	close-agent close --world --period --agent off  # books the bulk; emits proposals.json (the queue)
//	close-agent classify work  --world --period      # async worker (stub brain): proposals -> results.json
//	close-agent classify apply --world --period      # validate -> review -> derive -> post -> score (merges results in)
//
// The synchronous inline path (`close --agent replay|live`) is unchanged; this is
// the alternative async execution model where the agent runs decoupled from the close.

// newClassifyCmd is the parent for the async classify worker + apply stages (the
// queue is produced by `close --agent off`).
func newClassifyCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "classify",
		Short: "Asynchronous classify worker + apply (queue is emitted by `close --agent off`)",
	}
	cmd.AddCommand(newClassifyWorkCmd(out), newClassifyApplyCmd(out))
	return cmd
}

// resolveRoot returns the configured root or the working directory.
func resolveRoot(root, cmdName string) (string, error) {
	if root != "" {
		return root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("%s: resolve working directory: %w", cmdName, err)
	}
	return wd, nil
}

// newClassifyWorkCmd: `classify work` — the async WORK stage (stub brain).
func newClassifyWorkCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Run the async classify worker: proposals.json -> results.json (recover rate from order, cite the source)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRoot(root, "classify work")
			if err != nil {
				return err
			}
			n, err := classifyq.RunWorker(root, world, period)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "worked %d item(s) for world %q period %q -> %s\n", n, world, period, classifyq.ResultsPath(root, world, period))
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// newClassifyApplyCmd: `classify apply` — the deterministic APPLY stage.
func newClassifyApplyCmd(out io.Writer) *cobra.Command {
	var world, period, root, approvals string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Validate citations, review, derive money, post, reconcile, and score from results.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRoot(root, "classify apply")
			if err != nil {
				return err
			}
			opts := closer.ApplyOptions{}
			if approvals != "" {
				af, err := classifyq.ReadApprovals(approvals)
				if err != nil {
					return err
				}
				opts.Reviewer = classifyq.NewRecordedReviewer(af)
			}
			res, err := closer.RunApply(root, world, period, opts)
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
	cmd.Flags().StringVar(&approvals, "approvals", "", "path to an approvals.json (enables the recorded human-review gate; default: auto-approve)")
	return cmd
}
