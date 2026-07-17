package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/run"
	"github.com/spf13/cobra"
)

// context.go is the `ledger-flow context` command group — the read-only "close
// graph" surface (internal/readmodel) exposed as a terminal tool. It is BOTH the
// human investigation tool ("why didn't this clear?") and the agent's tool surface
// (option-2 CLI-as-tool): the §8 Flue agents reach the deterministic Go read model
// by shelling out to these commands and parsing their JSON, so there is one source
// of the projection and no second (drift-prone) reimplementation.
//
//	context breaks            --world --period   list reconcile break keys
//	context break  <key>      --world --period   the Tier-1 investigate bundle (JSON)
//	context event  <event-id> --world --period   the Tier-1 classify bundle  (JSON)
//
// Every command prints machine-readable JSON to stdout so the agent can consume it
// directly; the bundles carry the precomputed `booked` and recovered-rate facts
// (with citations) the agent needs. None of it reads truth (SPEC §4.4).
func newContextCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Read-only close-graph context bundles (the agent's tool surface)",
	}
	cmd.AddCommand(newContextBreaksCmd(out), newContextBreakCmd(out), newContextEventCmd(out), newContextEntityCmd(out))
	return cmd
}

// newContextEntityCmd: `context entity <id>` — the TIER-2 self-directed lookup:
// fetch ANY object by id (event raw snapshot, order with line items, rate-card
// channel, account balance) plus the derived facts (booked, graph edges). It is
// the agent's exploration tool for cases the Tier-1 bundles did not pre-solve.
func newContextEntityCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "entity <id>",
		Short: "Fetch any snapshotted object by id, with derived facts (JSON)",
		Long: "Fetch any object the period's snapshot knows, plus the graph's derived facts.\n\nResolvable id shapes:\n  " +
			strings.Join(recovery.EntityKinds, "\n  "),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveRoot(root)
			if err != nil {
				return err
			}
			g, err := run.BuildRecoveryEngine(r, world, period)
			if err != nil {
				return err
			}
			v, ok := g.Entity(args[0])
			if !ok {
				return fmt.Errorf("context entity: nothing named %q in %s/%s", args[0], world, period)
			}
			return printJSON(out, v)
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// resolveRoot returns the base directory containing worlds/: the explicit --root
// when set (tests), else the working directory (normal use).
func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("context: resolve working directory: %w", err)
	}
	return wd, nil
}

// printJSON writes v as indented JSON followed by a newline.
func printJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// newContextBreaksCmd: `context breaks` lists the canonical keys of the period's
// reconcile breaks, so a human or the agent can discover what to investigate.
func newContextBreaksCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "breaks",
		Short: "List the reconcile break keys for a period",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := resolveRoot(root)
			if err != nil {
				return err
			}
			m, err := run.BuildRecoveryEngine(r, world, period)
			if err != nil {
				return err
			}
			return printJSON(out, map[string][]string{"breaks": m.BreakKeys()})
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// newContextBreakCmd: `context break <key>` prints the Tier-1 investigate bundle for
// one break (the break, its settlement, the batch members with booked/recovered
// facts, candidates, applicable resolution entry types, and the receivable balance).
func newContextBreakCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "break <break-key>",
		Short: "Print the Tier-1 investigate context bundle for a break (JSON)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveRoot(root)
			if err != nil {
				return err
			}
			m, err := run.BuildRecoveryEngine(r, world, period)
			if err != nil {
				return err
			}
			b, ok := m.BreakContext(args[0])
			if !ok {
				return fmt.Errorf("context break: no break with key %q in %s/%s", args[0], world, period)
			}
			return printJSON(out, b)
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}

// newContextEventCmd: `context event <event-id>` prints the Tier-1 classify bundle
// for one event (the event, the recovered rate + citation when its own was stripped,
// and the applicable entry type).
func newContextEventCmd(out io.Writer) *cobra.Command {
	var world, period, root string
	cmd := &cobra.Command{
		Use:   "event <event-id>",
		Short: "Print the Tier-1 classify context bundle for an event (JSON)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveRoot(root)
			if err != nil {
				return err
			}
			m, err := run.BuildRecoveryEngine(r, world, period)
			if err != nil {
				return err
			}
			b, ok := m.EventContext(args[0])
			if !ok {
				return fmt.Errorf("context event: no event with id %q in %s/%s", args[0], world, period)
			}
			return printJSON(out, b)
		},
	}
	addWorldPeriodFlags(cmd, &world, &period)
	cmd.Flags().StringVar(&root, "root", "", "base directory containing worlds/ (defaults to the working directory)")
	_ = cmd.Flags().MarkHidden("root")
	return cmd
}
