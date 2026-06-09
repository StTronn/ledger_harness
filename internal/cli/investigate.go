package cli

import (
	"io"

	"github.com/razorpay/close-agent/internal/classifyq"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/spf13/cobra"
)

// investigate.go is the CLI for the ASYNC investigate pipeline (the §8 investigate
// seam), parallel to `classify`. A close run that ends with breaks parks them as
// breaks.json; `flue-agent investigate` (the TS agent) writes resolutions.json; and
// `investigate apply` rebuilds the books, applies the resolutions (validate citation
// -> derive money -> post), re-reconciles, and scores:
//
//	close-agent close --world --period --agent off   # parks proposals.json AND breaks.json
//	flue-agent  classify    --world --period          # results.json (classify long tail)
//	flue-agent  investigate --world --period          # resolutions.json (resolve breaks)
//	close-agent investigate apply --world --period     # apply resolutions -> reconcile -> score

func newInvestigateCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigate",
		Short: "Asynchronous investigate apply (queue is breaks.json; resolutions from `flue-agent investigate`)",
	}
	cmd.AddCommand(newInvestigateApplyCmd(out))
	return cmd
}

func newInvestigateApplyCmd(out io.Writer) *cobra.Command {
	var world, period, root, approvals string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Rebuild the books, apply investigate resolutions (validate->derive->post), reconcile, and score",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRoot(root, "investigate apply")
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
			res, err := closer.RunInvestigateApply(root, world, period, opts)
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
	cmd.Flags().StringVar(&approvals, "approvals", "", "path to an approvals.json (recorded review gate for the classify rebuild)")
	return cmd
}
