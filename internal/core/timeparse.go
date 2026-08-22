package core

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	timeRE      = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*([AP])\.?M\.?`)
	time24RE    = regexp.MustCompile(`\b([01]?\d|2[0-3]):([0-5]\d)\b`)
	fullDateRE  = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{2,4})\b`)
	shortDateRE = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})\b`)
	todayRE     = regexp.MustCompile(`(?i)\btoday\b`)
	yesterdayRE = regexp.MustCompile(`(?i)\byesterday\b`)

	// only real weekday words - a trailing "\w*" used to swallow "Monitoring",
	// "Satellite", "Wedge" and friends, back-dating a mail by up to six days
	weekdayRE = regexp.MustCompile(`(?i)\b(Mon|Monday|Tue|Tues|Tuesday|Wed|Wednesday|Thu|Thur|Thurs|Thursday|Fri|Friday|Sat|Saturday|Sun|Sunday)\b`)
)

// weekdayIndex uses Python's convention: Monday=0 ... Sunday=6.
var weekdayIndex = map[string]int{
	"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6,
}

var (
	dayFirstOnce     sync.Once
	dayFirstCached   bool
	dayFirstOverride *bool
)

// SetDayFirstForTest pins the date order, so a run on a US-format machine
// still exercises both orderings. Pass nil to go back to detection.
func SetDayFirstForTest(v *bool) { dayFirstOverride = v }

// DateIsDayFirst reports whether this machine writes dates as d/m/yyyy rather
// than m/d/yyyy.
//
// Outlook follows the Windows short-date setting, so "10/8/2026" means
// 10 August here and 8 October in the US. Guessing wrong reorders mail by
// months, which decides which one gets replied to.
func DateIsDayFirst() bool {
	if dayFirstOverride != nil {
		return *dayFirstOverride
	}
	dayFirstOnce.Do(func() { dayFirstCached = detectDayFirst() })
	return dayFirstCached
}

// dayMonth resolves two ambiguous date parts into (day, month).
func dayMonth(first, second int) (int, int) {
	if first > 12 {
		return first, second // only a day can exceed 12
	}
	if second > 12 {
		return second, first
	}
	if DateIsDayFirst() {
		return first, second
	}
	return second, first
}

// validDate builds a date and reports false if the parts do not describe a
// real day. Go's time.Date silently normalises 31 February into 3 March, so
// the result is checked rather than trusted.
func validDate(y, mo, d, hour, minute int) (time.Time, bool) {
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(mo), d, hour, minute, 0, 0, time.Local)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// timeSpan locates the clock time in a label, or nil.
func timeSpan(label string) []int {
	if m := timeRE.FindStringIndex(label); m != nil {
		return m
	}
	return time24RE.FindStringIndex(label)
}

// pickDate finds the date that is really the row's received field.
//
// A row label is the whole row - sender, subject, preview and the received
// time all run together - so matching a date anywhere in it took "24/7
// monitoring alert" as 24 July and "blocked 10/10 attempts" as 10 October,
// putting the wrong mail first and replying to it.
//
// Outlook writes the received field in one of two recognisable shapes: after
// the weekday ("Mon 8/10", "Wed 8/5/2026 1:10 PM") or directly beside the
// clock time. A date in neither position is a date in the subject, and this
// returns nil so the caller refuses rather than guesses.
func pickDate(label string, re *regexp.Regexp, timeAt []int) []string {
	all := re.FindAllStringIndex(label, -1)
	for _, at := range all {
		if anchoredByWeekday(label, at[0]) || besideTime(at, timeAt) {
			return re.FindStringSubmatch(label[at[0]:at[1]])
		}
	}
	// A label that is nothing BUT a timestamp needs no anchor - there is no
	// subject text it could have come from. This is the plain "8/13/2026" or
	// "Sat 7/11" form, and the conformance suite pins it.
	if len(all) == 1 && isBareTimestamp(label, all[0], timeAt) {
		return re.FindStringSubmatch(label[all[0][0]:all[0][1]])
	}
	return nil
}

// isBareTimestamp reports whether the label holds nothing except the date, the
// time and a weekday - no sender, no subject, no preview text for a stray
// number to hide in.
func isBareTimestamp(label string, date, timeAt []int) bool {
	cut := func(s string, span []int) string {
		if span == nil {
			return s
		}
		return s[:span[0]] + strings.Repeat(" ", span[1]-span[0]) + s[span[1]:]
	}
	rest := cut(cut(label, date), timeAt)
	rest = weekdayRE.ReplaceAllString(rest, " ")
	for _, r := range rest {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// anchoredByWeekday reports whether a weekday word ends just before start,
// which is how Outlook writes "Mon 8/10".
func anchoredByWeekday(label string, start int) bool {
	for _, wd := range weekdayRE.FindAllStringIndex(label, -1) {
		if gap := start - wd[1]; gap >= 0 && gap <= 2 {
			return true
		}
	}
	return false
}

// besideTime reports whether a date sits immediately next to the clock time,
// as in "8/5/2026 1:10 PM" or "1:10 PM 8/5/2026".
func besideTime(date, timeAt []int) bool {
	if timeAt == nil {
		return false
	}
	if gap := timeAt[0] - date[1]; gap >= 0 && gap <= 2 {
		return true
	}
	gap := date[0] - timeAt[1]
	return gap >= 0 && gap <= 2
}

// ParseRowTime is a best-effort timestamp from an Outlook row label.
//
// Handles the formats OWA actually shows: "7:34 PM" (today), "Mon 3:12 PM"
// (this week), "Sat 7/11" (this year), "7/4/2026". Reports ok=false when
// nothing parseable is present - callers must not guess an order from that.
func ParseRowTime(label string, now time.Time) (time.Time, bool) {
	if label == "" {
		return time.Time{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	hour, minute := 0, 0
	haveTime := false
	if m := timeRE.FindStringSubmatch(label); m != nil {
		h, _ := strconv.Atoi(m[1])
		hour = h % 12
		if m[3] == "P" || m[3] == "p" {
			hour += 12
		}
		minute, _ = strconv.Atoi(m[2])
		haveTime = true
	} else if m := time24RE.FindStringSubmatch(label); m != nil {
		// mailboxes set to a 24-hour clock
		hour, _ = strconv.Atoi(m[1])
		minute, _ = strconv.Atoi(m[2])
		haveTime = true
	}

	timeAt := timeSpan(label)

	// A date written in the row beats the words "today" and "yesterday". Those
	// used to be checked first, so "recap of yesterday Wed 8/5/2026" and
	// "Today's shift summary Wed 8/5/2026" were re-dated by months - and the
	// wrong mail in the thread was opened and replied to.
	if m := pickDate(label, fullDateRE, timeAt); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		d, mo := dayMonth(a, b)
		y, _ := strconv.Atoi(m[3])
		if y < 100 {
			y += 2000
		}
		t, ok := validDate(y, mo, d, hour, minute)
		if !ok {
			return time.Time{}, false
		}
		return t, true
	}
	if fullDateRE.MatchString(label) {
		// There IS a date, but nothing marks it as the received field, so it
		// cannot be told apart from one inside a subject line. Refusing is the
		// point: a caller must not order mail by a date of unknown meaning.
		return time.Time{}, false
	}

	if yesterdayRE.MatchString(label) {
		d := now.AddDate(0, 0, -1)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0,
			time.Local), true
	}
	if todayRE.MatchString(label) {
		return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0,
			time.Local), true
	}

	if m := pickDate(label, shortDateRE, timeAt); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		d, mo := dayMonth(a, b)
		// This year first, then last year - but an impossible day (29 February
		// in a common year) is refused outright rather than sliding quietly
		// into the previous year, matching the original.
		for _, year := range []int{now.Year(), now.Year() - 1} {
			t, ok := validDate(year, mo, d, hour, minute)
			if !ok {
				return time.Time{}, false
			}
			if !t.After(now.AddDate(0, 0, 1)) {
				return t, true
			}
		}
		return time.Time{}, false
	}
	if shortDateRE.MatchString(label) {
		// Same rule as the full date: an unanchored "24/7" or "10/10" in a
		// subject used to be read as the received date, back- or forward-dating
		// the mail by months.
		return time.Time{}, false
	}

	if !haveTime {
		return time.Time{}, false
	}

	day := now
	if wd := weekdayRE.FindStringSubmatch(label); wd != nil {
		name := wd[1]
		if len(name) > 3 {
			name = name[:3]
		}
		target := weekdayIndex[lowerASCII(name)]
		// Python counts Monday=0; Go counts from Sunday.
		nowIdx := (int(now.Weekday()) + 6) % 7
		delta := ((nowIdx-target)%7 + 7) % 7
		if delta == 0 {
			// Outlook labels a mail by weekday only when it is not today, so a
			// matching weekday name means a week ago, not this morning
			delta = 7
		}
		day = now.AddDate(0, 0, -delta)
	}

	stamp := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0,
		time.Local)
	if stamp.After(now.Add(5 * time.Minute)) {
		// a time later today means it arrived yesterday, not in the future
		stamp = stamp.AddDate(0, 0, -1)
	}
	return stamp, true
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Timed pairs an item with the label its timestamp is read from.
type Timed[T any] struct {
	Label string
	Item  T
}

// SortNewestFirst orders result rows newest-first by their displayed
// timestamp. Rows with no parseable time keep their original order, after the
// rest - never interleaved, so an unreadable row cannot displace a dated one.
func SortNewestFirst[T any](items []Timed[T], now time.Time) []T {
	if now.IsZero() {
		now = time.Now()
	}
	type row struct {
		undated int
		stamp   time.Time
		index   int
		item    T
	}
	rows := make([]row, 0, len(items))
	for i, it := range items {
		r := row{undated: 1, index: i, item: it.Item}
		if ts, ok := ParseRowTime(it.Label, now); ok {
			r.undated, r.stamp = 0, ts
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].undated != rows[b].undated {
			return rows[a].undated < rows[b].undated
		}
		if rows[a].undated == 0 && !rows[a].stamp.Equal(rows[b].stamp) {
			return rows[a].stamp.After(rows[b].stamp)
		}
		return rows[a].index < rows[b].index
	})
	out := make([]T, len(rows))
	for i, r := range rows {
		out[i] = r.item
	}
	return out
}
