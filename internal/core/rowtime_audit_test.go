package core

import (
	"testing"
	"time"
)

// A row label is the whole row run together - sender, subject, preview and the
// received time. Matching a date anywhere in it meant subject text re-dated
// the mail, so SortNewestFirst put the wrong one first and ProcessCase opened
// it while logging "replying to the most recent one".
//
// The rule now: a date counts as the received field only when Outlook's own
// formatting marks it as one - after the weekday, beside the clock, or alone
// in a label that holds nothing else. Anything else is refused, because a date
// of unknown meaning must not decide which mail gets the reply.
func TestADateInTheSubjectDoesNotRedateTheMail(t *testing.T) {
	dayFirst := false
	SetDayFirstForTest(&dayFirst) // month-first, as the audit measured it
	defer SetDayFirstForTest(nil)

	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.Local)

	cases := []struct {
		label string
		want  string // "" means it must refuse
		why   string
	}{
		// The received field is weekday-anchored, so the subject's words lose.
		{"SOC Alerts SOC610529 Today's shift summary Wed 8/5/2026 1:10 PM",
			"2026-08-05T13:10", "an explicit date beats the word \"today\""},
		{"SOC Alerts SOC610529 recap of yesterday Wed 8/5/2026 1:10 PM",
			"2026-08-05T13:10", "an explicit date beats the word \"yesterday\""},

		// Numbers that look like dates but are plainly subject text.
		{"SOC Alerts SOC610529 24/7 monitoring alert 3:15 PM", "",
			"24/7 is not a date this mail arrived on"},
		{"SOC Alerts SOC610529 blocked 10/10 attempts 4:00 PM", "",
			"10/10 attempts is not a received date"},
		{"SOC Alerts SOC610529 50/50 split rule 4:00 PM", "",
			"50/50 is not a date at all"},

		// The shapes Outlook really writes, which must still parse. The first
		// has the layout of a label captured from a live mailbox - note the
		// received time sits in the MIDDLE, with preview text after it, which
		// is why a tail-only read of the timestamp would break every real row.
		{"Unread Collapsed Sam Rivera SOC 700020 Suspicious login from " +
			"unusual location Mon 8/10 No preview is available",
			"2026-08-10T00:00", "a real OWA row: the date sits mid-label"},
		{"Jordan Lee SOC610529 Suspicious login Wed 8/5/2026 1:10 PM",
			"2026-08-05T13:10", "weekday, date and time together"},
		{"8/13/2026", "2026-08-13T00:00", "a label that is only a date"},
		{"Sat 7/11", "2026-07-11T00:00", "weekday and short date"},
		{"7:34 PM", "2026-08-09T19:34", "time only means today"},
	}

	for _, c := range cases {
		got, ok := ParseRowTime(c.label, now)
		if c.want == "" {
			if ok {
				t.Errorf("%s\n  label %q\n  parsed as %s, want a refusal",
					c.why, c.label, got.Format("2006-01-02T15:04"))
			}
			continue
		}
		if !ok {
			t.Errorf("%s\n  label %q\n  refused, want %s", c.why, c.label, c.want)
			continue
		}
		if s := got.Format("2006-01-02T15:04"); s != c.want {
			t.Errorf("%s\n  label %q\n  got %s, want %s", c.why, c.label, s, c.want)
		}
	}
}

// Ordering is the reason any of this matters: the tool opens ordered[0] and
// says "replying to the most recent one".
func TestSubjectDatesDoNotReorderAThread(t *testing.T) {
	dayFirst := false
	SetDayFirstForTest(&dayFirst)
	defer SetDayFirstForTest(nil)

	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.Local)
	rows := []Timed[string]{
		{Label: "SOC Alerts SOC610529 the original alert Mon 8/3/2026 9:00 AM",
			Item: "oldest"},
		{Label: "Sam Rivera SOC610529 24/7 monitoring update Fri 8/7/2026 6:30 PM",
			Item: "newest"},
	}
	got := SortNewestFirst(rows, now)
	if len(got) == 0 {
		t.Fatal("everything was dropped")
	}
	if got[0] != "newest" {
		t.Errorf("ordered %v first; \"24/7\" in a subject re-dated the mail and "+
			"the reply would go on the wrong message", got[0])
	}
}
