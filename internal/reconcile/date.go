package reconcile

import (
	"fmt"
	"time"
)

// This file holds the small date arithmetic the checks need: parsing a
// YYYY-MM-DD value-date string and measuring the day gap between two dates for
// the check #1 tolerance and the check #3 in-transit cutoff. It uses time.Parse
// in UTC as a pure calendar calculator — it NEVER reads time.Now(), so the
// reconcile stage stays deterministic (SPEC §12). Money is untouched here; this
// is dates only.

// dateLayout is the value-date format used on bank-feed entries and settlement
// dates throughout the substrate (SPEC §4.4 "YYYY-MM-DD").
const dateLayout = "2006-01-02"

// parseDate parses a YYYY-MM-DD string into a UTC time. A malformed date is
// surfaced as an error so a check can degrade safely (treat it as unmatched)
// rather than silently mis-comparing.
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reconcile: invalid date %q: %w", s, err)
	}
	return t.UTC(), nil
}

// dayGap returns |a − b| in whole days between two YYYY-MM-DD dates. It returns
// (gap, true) on success and (0, false) if either date is unparseable, so a
// caller can decide how to treat an undated/garbled line (the checks treat it as
// a non-match for tolerance purposes). The result is symmetric and never
// negative.
func dayGap(a, b string) (int, bool) {
	ta, err := parseDate(a)
	if err != nil {
		return 0, false
	}
	tb, err := parseDate(b)
	if err != nil {
		return 0, false
	}
	const secsPerDay = 24 * 60 * 60
	diff := ta.Unix() - tb.Unix()
	if diff < 0 {
		diff = -diff
	}
	return int(diff / secsPerDay), true
}

// dateString renders a Unix-seconds instant as a YYYY-MM-DD string in UTC — the
// settlement's value date in the same format the bank-feed lines carry, so the
// two can be compared by dayGap. It mirrors the seeder's own date formatting but
// is defined here independently: reconcile must NOT import internal/seed (SPEC
// §4.4 boundaries), so it reuses only the stdlib. It is float-free and
// deterministic (UTC, no wall clock).
func dateString(epoch int64) string {
	t := time.Unix(epoch, 0).UTC()
	return t.Format(dateLayout)
}

// onOrAfter reports whether date d is on or after the cutoff (both YYYY-MM-DD).
// It is used by check #3 to classify a settlement's bank credit as genuine T+2
// in-transit (credit lands on/after the period-end cutoff). An unparseable date
// or empty cutoff returns false (treated as in-period), so a missing period
// bound never wrongly excuses a residual.
func onOrAfter(d, cutoff string) bool {
	if d == "" || cutoff == "" {
		return false
	}
	td, err := parseDate(d)
	if err != nil {
		return false
	}
	tc, err := parseDate(cutoff)
	if err != nil {
		return false
	}
	return !td.Before(tc)
}
