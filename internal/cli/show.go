package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// traceFileName is the file an agent trajectory is recorded to under a run path
// (Phase 7+). In Phase 6 — the deterministic, agent-free product — no traces are
// produced, so `show trace` resolves this name and reports its absence GRACEFULLY
// rather than crashing (the gate's "handles a missing trace gracefully").
const traceFileName = "trace.json"

// newShowTraceCmd: `close-agent show trace <path>` — prints the recorded agent
// trajectory under the given run path. The agent (and therefore traces) lands in
// Phase 7; in the Phase-6 deterministic product there are no traces, so a missing
// trace is the EXPECTED, non-error outcome and is reported as such (not a crash,
// not a stack trace). When a path points directly at a trace file, that file is
// read; when it points at a run directory, traceFileName inside it is read.
func newShowTraceCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "trace <path>",
		Short: "Print an agent trajectory from a run path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showTrace(out, args[0])
		},
	}
}

// showTrace resolves the trace artifact at path (a file, or traceFileName inside
// a directory) and pretty-prints it. A missing OR EMPTY trace is handled
// gracefully: it prints a clear "no trace found / no trace available (agent
// phases not run)" message and returns nil (no error, no crash, exit 0), because
// in the agent-free Phase-6 product no trace exists yet — traces are produced by
// the agent in Phase 7+. A genuine read error (e.g. a permission failure on a
// file that does exist) is surfaced as an error.
func showTrace(out io.Writer, path string) error {
	resolved := resolveTracePath(path)

	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			printNoTrace(out, path, resolved)
			return nil
		}
		return fmt.Errorf("show trace: read %s: %w", resolved, err)
	}
	// An empty (or whitespace-only) trace file is the same EXPECTED, non-error
	// outcome as a missing one: the agent has not run, so there is nothing to
	// show. Treat it like an absent trace rather than emitting blank output.
	if len(bytes.TrimSpace(data)) == 0 {
		printNoTrace(out, path, resolved)
		return nil
	}

	// A trace exists (Phase 7+). Pretty-print it for human inspection. The trace
	// schema is frozen when the agent lands; here we only need to render whatever
	// was recorded. If it is valid JSON, re-indent it (2-space, no HTML escaping)
	// so a trajectory is readable; if it is not JSON (a future/older format), fall
	// back to printing the raw bytes verbatim rather than failing.
	pretty, ok := prettyJSON(data)
	if !ok {
		pretty = data
	}
	if _, err := out.Write(pretty); err != nil {
		return fmt.Errorf("show trace: write %s: %w", resolved, err)
	}
	if len(pretty) > 0 && pretty[len(pretty)-1] != '\n' {
		fmt.Fprintln(out)
	}
	return nil
}

// printNoTrace emits the graceful, non-error message for an absent or empty
// trace. It keeps the literal "no trace found" phrase (the gate / unit test
// assert it) and adds the clear "no trace available (agent phases not run)"
// explanation the SPEC §10 / Phase-6 task calls for.
func printNoTrace(out io.Writer, path, resolved string) {
	fmt.Fprintf(out, "show trace %s: no trace found at %s\n", path, resolved)
	fmt.Fprintf(out, "  no trace available (agent phases not run)\n")
	fmt.Fprintf(out, "  (traces are produced by the agent in Phase 7+; "+
		"the deterministic Phase-6 product writes none)\n")
}

// prettyJSON re-indents raw JSON bytes to the canonical 2-space, no-HTML-escape
// form for display, returning (formatted, true) on success. If data is not valid
// JSON it returns (nil, false) so the caller can fall back to verbatim output.
func prettyJSON(data []byte) ([]byte, bool) {
	if !json.Valid(data) {
		return nil, false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if err := enc.Encode(v); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// resolveTracePath returns the trace file to read for the given argument: if the
// argument is a directory (a run dir), the trace file inside it; otherwise the
// argument itself (a path pointing straight at a trace file). A non-existent
// argument is treated as a file path so the caller's missing-trace handling
// produces the graceful message.
func resolveTracePath(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, traceFileName)
	}
	return path
}
