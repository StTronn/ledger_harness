package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/razorpay/close-agent/internal/agentclient"
	"github.com/razorpay/close-agent/internal/closer"
	"github.com/spf13/cobra"
)

// newCloseCmd: `close-agent close --world <string> --period <YYYY-MM>` (SPEC §10).
// It runs the deterministic close pipeline end to end (ingest → normalize →
// classify → bind+post) over the period's fixtures and scores the produced ledger
// against the hidden truth GL, printing the score plus the classified/skipped
// counts (SPEC §2 Phase 4, §5, §9).
//
// The --agent flag selects what happens on a rule miss (SPEC §5, §8, §11
// Phase 7a): "off" (the default, the Phase-4 flag-and-skip baseline — events the
// rule engine cannot classify are FLAGGED and SKIPPED, reported, never crashed)
// or "replay" (consult the §8 classify agent from the committed, deterministic
// recorded-response fixture — no LLM in CI). On the clean dtc/2026-05 period every
// event classifies under either mode and the score is 100%. On the hard
// dtc/2026-04 period --agent off scores PARTIAL (the recorded baseline) and
// --agent replay RISES to ~100% by classifying the missed payments from the
// recorded responses (recovered from orders.json, not truth).
func newCloseCmd(out io.Writer) *cobra.Command {
	var world, period, root, agent, liveURL string
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Run the close workflow for a period and print the score",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// worlds/ live under <root>; default to the working directory (the
			// module root in normal use). The hidden --root flag lets tests point at
			// a temp-seeded period without touching the SPEC §10 flag surface.
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("close: resolve working directory: %w", err)
				}
				root = wd
			}
			res, err := closer.RunWith(root, world, period, closer.Options{
				Agent:       agentclient.Mode(agent),
				LiveBaseURL: liveURL,
			})
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
	cmd.Flags().StringVar(&agent, "agent", "off",
		"classify-agent mode for rule misses: off (Phase-4 baseline) | replay (committed recorded responses, CI-safe) | live (Flue endpoint, not for CI)")
	cmd.Flags().StringVar(&liveURL, "agent-url", "", "Flue agent base URL (only used with --agent live)")
	_ = cmd.Flags().MarkHidden("agent-url")
	return cmd
}

// printCloseResult prints a concise close summary: the classified/skipped counts,
// any skipped events (flagged, not crashed), and the score against truth. The
// score line ("score = N%") is the deterministic baseline the gate checks.
func printCloseResult(out io.Writer, world, period string, res closer.Result) {
	fmt.Fprintf(out, "close world %q period %q\n", world, period)
	fmt.Fprintf(out, "  agent mode: %s\n", res.AgentMode)
	fmt.Fprintf(out, "  classified: %d events -> %d posted entries\n", res.Classified, res.Ledger.Len())
	// Report what the agent recovered on the rule misses (Phase 7a). On the agent-
	// off baseline this is 0; on a replay run it is the count the agent classified.
	if res.AgentDone > 0 || res.AgentMode != "off" {
		fmt.Fprintf(out, "  agent classified: %d rule-missed events\n", res.AgentDone)
	}
	fmt.Fprintf(out, "  skipped:    %d events\n", len(res.Skipped))
	for _, s := range res.Skipped {
		fmt.Fprintf(out, "    - %s %s: %s\n", s.Type, s.EventID, s.Reason)
	}
	// The skipped events are PARKED as the async classify work queue (SPEC §8 async
	// seam): run `classify work` then `classify apply` to process them out of band.
	if res.ProposalsPath != "" {
		fmt.Fprintf(out, "  async work queue: %s (%d item(s); run `classify work` then `classify apply`)\n",
			res.ProposalsPath, len(res.Skipped))
	}

	sc := res.Score
	tb := "no"
	if sc.TrialBalanceMatches {
		tb = "yes"
	}
	// Reconciliation (SPEC §7, §8): list the breaks left UNRESOLVED after the
	// investigate agent ran. On the clean period this is "0 breaks (reconciled)".
	// When the §8 investigate agent resolved breaks by posting (Phase 8), report
	// how many it fixed; breaks it escalated (or any break with the agent off) are
	// listed as remaining, never guessed.
	if res.InvestigateDone > 0 {
		fmt.Fprintf(out, "  investigate: resolved %d break(s) by posting\n", res.InvestigateDone)
	}
	if len(res.Breaks) == 0 {
		fmt.Fprintf(out, "  reconcile: 0 breaks (reconciled)\n")
	} else {
		fmt.Fprintf(out, "  reconcile: %d break(s)\n", len(res.Breaks))
		for _, b := range res.Breaks {
			where := b.SettlementID
			if where == "" {
				where = "(period)"
			}
			fmt.Fprintf(out, "    - check#%d %s [%s] expected=%s actual=%s candidates=%v\n",
				b.Check, b.Kind, where, b.Expected, b.Actual, b.CandidateEventIDs)
			fmt.Fprintf(out, "        %s\n", b.Detail)
		}
	}
	for _, e := range res.Escalations {
		fmt.Fprintf(out, "    escalated [%s]: %s\n", e.Kind, e.Reason)
	}

	fmt.Fprintf(out, "  trial balance matches truth: %s\n", tb)
	fmt.Fprintf(out, "  entries correct: %d/%d\n", sc.Correct, sc.Total)
	if len(sc.Errors) > 0 {
		fmt.Fprintf(out, "  scoring errors: %d\n", len(sc.Errors))
		for _, e := range sc.Errors {
			fmt.Fprintf(out, "    - [%s] %s\n", e.Class, e.EventID)
		}
	}
	// Report the frozen errors.json artifact path (SPEC §9, §10) so the operator
	// knows the single learning seam was written for this run.
	if res.ErrorsPath != "" {
		fmt.Fprintf(out, "  errors record: %s (schema v%d)\n", res.ErrorsPath, res.Record.SchemaVersion)
	}
	// Report the frozen trace artifact path (SPEC §9, §10, §13) when the agent ran
	// and emitted traces, so `show trace` knows where to look.
	if res.TracePath != "" {
		fmt.Fprintf(out, "  agent traces: %s (schema v%d, %d trace(s))\n",
			res.TracePath, agentTraceSchemaVersion(res.Traces), len(res.Traces))
	}
	if res.InvestigateTracePath != "" {
		fmt.Fprintf(out, "  investigate traces: %s (schema v%d, %d trace(s))\n",
			res.InvestigateTracePath, agentclient.InvestigateTraceSchemaVersion, len(res.InvestigateTraces))
	}
	fmt.Fprintf(out, "score = %d%%\n", sc.Percent())
}

// agentTraceSchemaVersion returns the frozen trace schema version for reporting.
// It reads it off the first trace (all traces share TraceSchemaVersion); with no
// traces it falls back to the package constant so the reported version is always
// the frozen one.
func agentTraceSchemaVersion(traces []agentclient.Trace) int {
	if len(traces) > 0 {
		return traces[0].SchemaVersion
	}
	return agentclient.TraceSchemaVersion
}
