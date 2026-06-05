package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/spf13/cobra"
)

// newRecordResponsesCmd is the HIDDEN, deterministic generator for the committed
// recorded-response fixture (SPEC §2, §12):
//
//	close-agent record-responses --world dtc --period 2026-04
//
// It rebuilds worlds/<world>/<period>/agent/classify.recorded.json by, for each
// rule-missed payment, fetching its order from orders.json (the legitimate
// recovery source, NOT truth) and producing the same {entry_type: dtc_sale,
// params} the rule engine would have produced had the gst_rate been present
// (internal/agentclient.GenerateRecorded). It is deterministic and reproducible —
// running it on a committed period reproduces the committed file byte-for-byte —
// so the recorded responses are reviewable and never hand-typed.
//
// It is hidden because it is a maintenance/regeneration tool, not part of the
// SPEC §10 operator surface; the gate exercises it via close --agent replay and a
// unit test. It NEVER reads truth.
func newRecordResponsesCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:    "record-responses",
		Short:  "Regenerate the committed classify.recorded.json from orders.json (recovery source)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("record-responses: resolve working directory: %w", err)
				}
				root = wd
			}
			f, err := agentclient.GenerateRecorded(root, world, period)
			if err != nil {
				return err
			}
			path := agentclient.RecordedPath(root, world, period)
			if err := agentclient.WriteRecorded(path, f); err != nil {
				return err
			}
			fmt.Fprintf(out, "recorded %d agent response(s) for world %q period %q -> %s\n",
				len(f.Responses), world, period, path)
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}
