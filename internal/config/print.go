package config

import (
	"fmt"
	"io"
	"strings"
)

// Print writes a human-readable rendering of the playbook to w: the chart of
// accounts grouped by root (with each account's normal balance) followed by the
// entry-type templates and their balanced lines. It is a pure function of the
// loaded Playbook — no side state — so `close-agent show playbook` doubles as
// proof the file loaded and validated.
func (p *Playbook) Print(w io.Writer) error {
	bw := &errWriter{w: w}

	bw.printf("CHART OF ACCOUNTS (%d accounts)\n", len(p.Accounts))
	for _, group := range p.AccountsByRoot() {
		if len(group.Accounts) == 0 {
			continue
		}
		bw.printf("  %s/   [normal balance: %s]\n", group.Root, longSide(group.Root.NormalBalance()))
		for _, a := range group.Accounts {
			// Print the path relative to its root for a tree-like look.
			leaf := strings.TrimPrefix(a.Path, string(group.Root)+"/")
			if a.Note != "" {
				bw.printf("    %-34s  %s\n", leaf, a.Note)
			} else {
				bw.printf("    %s\n", leaf)
			}
		}
	}

	bw.printf("\nENTRY TYPES (%d templates)\n", len(p.EntryTypes))
	for _, e := range p.EntryTypes {
		bw.printf("  %s  params{%s}\n", e.Name, strings.Join(e.Params, ", "))
		if e.Doc != "" {
			bw.printf("    %s\n", e.Doc)
		}
		if e.TxParam != "" {
			bw.printf("    tx: %s\n", e.TxParam)
		}
		for _, l := range e.Lines {
			bw.printf("    %-2s %-40s %s\n", l.Side, l.Account, l.Amount)
		}
	}
	return bw.err
}

// longSide renders a Side for human output.
func longSide(s Side) string {
	switch s {
	case Debit:
		return "Debit"
	case Credit:
		return "Credit"
	default:
		return string(s)
	}
}

// errWriter is a tiny io.Writer wrapper that records the first write error and
// short-circuits subsequent writes, so Print stays linear and still surfaces I/O
// failures.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
