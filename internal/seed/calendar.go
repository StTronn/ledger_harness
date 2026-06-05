package seed

import (
	"fmt"
	"time"
)

// This file turns a YYYY-MM period plus an in-month day index into deterministic
// timestamps. It uses time.Date in UTC purely as a calendar calculator (days in
// month, day -> epoch) — it NEVER reads time.Now(), so there is no wall-clock and
// the output is reproducible (SPEC §2, §12).

// periodCalendar holds a parsed period and its day bounds so the generator can
// place events on specific days without re-parsing each time.
type periodCalendar struct {
	year       int
	month      time.Month
	daysInMon  int
	periodStr  string // original YYYY-MM
	startEpoch int64  // Unix seconds at YYYY-MM-01T00:00:00Z
}

// newPeriodCalendar parses a validated YYYY-MM string into a periodCalendar. The
// period must already have passed ValidatePeriod; this re-derives the integers.
func newPeriodCalendar(period string) (periodCalendar, error) {
	if err := ValidatePeriod(period); err != nil {
		return periodCalendar{}, err
	}
	year := int(period[0]-'0')*1000 + int(period[1]-'0')*100 + int(period[2]-'0')*10 + int(period[3]-'0')
	monthNum := int(period[5]-'0')*10 + int(period[6]-'0')
	month := time.Month(monthNum)

	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	// Days in month, computed in integer space (no float): the seconds between
	// the 1st of this month and the 1st of next month divided by a day's seconds.
	nextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
	const secsPerDay = 24 * 60 * 60
	days := int((nextMonth.Unix() - start.Unix()) / secsPerDay)

	return periodCalendar{
		year:       year,
		month:      month,
		daysInMon:  days,
		periodStr:  period,
		startEpoch: start.Unix(),
	}, nil
}

// epochForDay returns the Unix seconds at the start (00:00:00Z) of the given
// 1-based day-of-month within the period. day is clamped into [1, daysInMon] so
// a draw that overshoots the month (e.g. a settlement date a few days past
// month end) lands on the last valid day rather than escaping the period.
func (c periodCalendar) epochForDay(day int) int64 {
	if day < 1 {
		day = 1
	}
	// Note: a settlement T+2 may land in the next month; we still allow up to
	// daysInMon+3 by extending via seconds, see epochForDayOffset.
	if day > c.daysInMon {
		day = c.daysInMon
	}
	return c.startEpoch + int64(day-1)*86400
}

// epochForDayOffset returns the Unix seconds at 00:00:00Z, dayOffset days after
// the period start (0 = the 1st). Unlike epochForDay it does NOT clamp, so a
// settlement landing a day or two past month-end gets a real, later timestamp.
func (c periodCalendar) epochForDayOffset(dayOffset int) int64 {
	return c.startEpoch + int64(dayOffset)*86400
}

// dateString renders a Unix-seconds instant as a YYYY-MM-DD string in UTC, the
// value-date format used on bank-feed entries (SPEC §4.4). It is float-free and
// deterministic.
func dateString(epoch int64) string {
	t := time.Unix(epoch, 0).UTC()
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}
