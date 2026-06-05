package score

import (
	"sort"
	"strings"

	"github.com/razorpay/close-agent/internal/truth"
)

// render.go produces the short, human-readable Got/Want strings stamped on an
// ErrorRecord. They exist so a diff or error report is self-contained — a person
// (or the diff command) can read what was booked vs what was expected without
// re-loading either ledger. The renderings are deterministic (lines sorted by
// side/account/amount) so identical entries render to identical strings and the
// error records are byte-stable across runs (SPEC §9 determinism).

// renderProduced renders a produced entry as "entry_type: Dr account amount, …",
// with its lines in a canonical order.
func renderProduced(p Produced) string {
	lines := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		lines[i] = l.Side + " " + l.Account + " " + l.Amount.String()
	}
	return p.EntryType + ": " + joinSortedLines(lines)
}

// renderTruth renders a truth entry the same way as renderProduced, so a wrong
// entry's Got and Want lines line up for an eyeball diff.
func renderTruth(e truth.Entry) string {
	lines := make([]string, len(e.Lines))
	for i, l := range e.Lines {
		lines[i] = string(l.Side) + " " + l.Account + " " + l.Amount.String()
	}
	return e.EntryType + ": " + joinSortedLines(lines)
}

// joinSortedLines sorts the rendered line strings and joins them with ", " so the
// rendering is independent of posting order within an entry.
func joinSortedLines(lines []string) string {
	sort.Strings(lines)
	return strings.Join(lines, ", ")
}
