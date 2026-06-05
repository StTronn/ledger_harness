package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/closer"
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

// newRecordInvestigationsCmd is the HIDDEN, deterministic generator for the
// committed recorded-INVESTIGATION fixture (SPEC §2, §8, §12), parallel to
// record-responses for classify:
//
//	close-agent record-investigations --world dtc --period 2026-03
//
// It rebuilds worlds/<world>/<period>/agent/investigate.recorded.json by running
// the close pipeline up to reconcile (with the committed classify replay) and, for
// each break, deriving the resolution from the snapshotted agent-input fixtures
// (orders.json / refunds.json, NOT truth) — the same {entry_type, params} the rule
// engine would have produced for the unbooked refund (closer.GenerateInvestigateRecorded).
// It is deterministic and reproducible; a test asserts it reproduces the committed
// file byte-for-byte. It NEVER reads truth.
func newRecordInvestigationsCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:    "record-investigations",
		Short:  "Regenerate the committed investigate.recorded.json from the snapshotted fixtures (recovery source)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("record-investigations: resolve working directory: %w", err)
				}
				root = wd
			}
			f, err := closer.GenerateInvestigateRecorded(root, world, period)
			if err != nil {
				return err
			}
			path := agentclient.InvestigateRecordedPath(root, world, period)
			if err := agentclient.WriteInvestigateRecorded(path, f); err != nil {
				return err
			}
			fmt.Fprintf(out, "recorded %d investigation(s) for world %q period %q -> %s\n",
				len(f.Resolutions), world, period, path)
			return nil
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}
