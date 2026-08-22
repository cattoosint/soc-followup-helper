package core

import (
	"testing"
	"time"
)

// Every case here comes from a defect found by review of the live behaviour of
// the Python original. Auto-send puts email in front of real recipients with
// no human check, so every one of these must fail closed - when anything is
// ambiguous the case goes back to the analyst.

const (
	alerts = "alerts@example.com"
	phrase = "follow up"
	body   = "SOC700001 alert\nPlease follow up on this case."
)

// --- the sender must come from the real sender line ----------------------

func TestQuotedFromDoesNotPassAsSender(t *testing.T) {
	quoted := "Jane Colleague Thu 8/7/2026 10:15 AM FYI - " +
		"From: SOC Alerts <alerts@example.com> Sent: Wed 8/6/2026 " +
		"To: Jane Subject: SOC700001"
	sent, why := ShouldAutoSend(SenderPart(quoted), body, phrase, alerts)
	if sent {
		t.Fatalf("a quoted From: passed as the sender: sender_part=%q why=%q",
			SenderPart(quoted), why)
	}
}

func TestLowercaseRecipientDoesNotPassAsSender(t *testing.T) {
	lowerTo := "Jane Colleague to: alerts@example.com Thu 8/7/2026 10:15 AM"
	sent, why := ShouldAutoSend(SenderPart(lowerTo), body, phrase, alerts)
	if sent {
		t.Fatalf("a lowercase to: passed as the sender: sender_part=%q why=%q",
			SenderPart(lowerTo), why)
	}
}

func TestOnlyQuotedHeaderYieldsNoSender(t *testing.T) {
	startsQuoted := "From: SOC Alerts <alerts@example.com> Sent: Wed 8/6/2026"
	if got := SenderPart(startsQuoted); got != "" {
		t.Fatalf("want empty sender, got %q", got)
	}
}

func TestGenuineSenderLineStillMatches(t *testing.T) {
	real := "AS ISS SOC ALERTS<alerts@example.com>   To:Analyst Wed 8/5/2026 1:10 PM"
	sent, why := ShouldAutoSend(SenderPart(real), body, phrase, alerts)
	if !sent {
		t.Fatalf("a genuine sender line was refused: %s", why)
	}
}

func TestBodyTextAfterTimestampIsNotTheSender(t *testing.T) {
	preview := "AS ISS SOC ALERTS<alerts@example.com> Wed 8/5/2026 1:10 PM " +
		"Please follow up - regards, Jane Colleague"
	got := NormalizeText(SenderPart(preview))
	if contains(got, "jane") {
		t.Fatalf("body text leaked into the sender: %q", SenderPart(preview))
	}
}

// --- configuration sanity -------------------------------------------------

func TestVeryShortConfiguredSenderIsRefused(t *testing.T) {
	sent, why := ShouldAutoSend("AS ISS SOC ALERTS<alerts@example.com>",
		body, phrase, "SOC")
	if sent {
		t.Fatalf("a 3-character sender was accepted: %s", why)
	}
}

func TestMissingConfigurationRefuses(t *testing.T) {
	for _, c := range []struct{ name, keyword, sender string }{
		{"no phrase", "", alerts},
		{"no sender", phrase, ""},
	} {
		if sent, _ := ShouldAutoSend("SOC Alerts <alerts@example.com>",
			body, c.keyword, c.sender); sent {
			t.Fatalf("%s: auto-send fired without full configuration", c.name)
		}
	}
}

func TestUnreadableMessageRefuses(t *testing.T) {
	if sent, _ := ShouldAutoSend("SOC Alerts <alerts@example.com>", "",
		phrase, alerts); sent {
		t.Fatal("auto-send fired with an unreadable body")
	}
	if sent, _ := ShouldAutoSend("", body, phrase, alerts); sent {
		t.Fatal("auto-send fired with no identifiable sender")
	}
}

func TestWrappedAddressStillMatches(t *testing.T) {
	// Outlook breaks long addresses mid-word for wrapping
	wrapped := "AS ISS SOC ALERTS<alerts@ example.com> 1:10 PM"
	if sent, why := ShouldAutoSend(SenderPart(wrapped), body, phrase,
		alerts); !sent {
		t.Fatalf("a wrapped address was refused: %s", why)
	}
}

// --- timestamp reading ----------------------------------------------------

// now is a Sunday evening, matching the Python suite.
var now = time.Date(2026, 8, 9, 20, 0, 0, 0, time.Local)

func TestNowIsSunday(t *testing.T) {
	if now.Weekday() != time.Sunday {
		t.Fatalf("fixture drifted: %s is a %s, not Sunday", now, now.Weekday())
	}
}

func TestMonitoringIsNotMonday(t *testing.T) {
	ts, ok := ParseRowTime("Monitoring alert 9:15 AM", now)
	if !ok || ts.Day() != now.Day() {
		t.Fatalf("'Monitoring' was read as a weekday: %v", ts)
	}
}

func TestSatelliteIsNotSaturday(t *testing.T) {
	ts, ok := ParseRowTime("Satellite feed 9:15 AM", now)
	if !ok || ts.Day() != now.Day() {
		t.Fatalf("'Satellite' was read as a weekday: %v", ts)
	}
}

func TestTodaysWeekdayMeansAWeekAgo(t *testing.T) {
	ts, ok := ParseRowTime("Sun 9:15 AM", now)
	if !ok {
		t.Fatal("could not parse 'Sun 9:15 AM'")
	}
	want := time.Date(2026, 8, 2, 9, 15, 0, 0, time.Local)
	if !ts.Equal(want) {
		t.Fatalf("want %v (a week back), got %v", want, ts)
	}
}

func TestYesterdayIsYesterday(t *testing.T) {
	ts, ok := ParseRowTime("Yesterday 11:30 PM", now)
	if !ok || now.Day()-ts.Day() != 1 {
		t.Fatalf("'Yesterday' misread: %v", ts)
	}
}

func TestTwentyFourHourClockIsUnderstood(t *testing.T) {
	ts, ok := ParseRowTime("14:35", now)
	if !ok || ts.Hour() != 14 || ts.Minute() != 35 {
		t.Fatalf("24-hour clock misread: %v", ts)
	}
}

func TestTimeLaterTodayIsYesterdayNotTheFuture(t *testing.T) {
	ts, ok := ParseRowTime("11:30 PM", now)
	if !ok || ts.After(now) {
		t.Fatalf("a later time was read as the future: %v", ts)
	}
}

func TestDayAboveTwelveForcesDayMonthOrder(t *testing.T) {
	ts, ok := ParseRowTime("13/8/2026", now)
	if !ok || ts.Day() != 13 || ts.Month() != time.August {
		t.Fatalf("want 13 August, got %v", ts)
	}
}

func TestMonthAboveTwelveForcesMonthDayOrder(t *testing.T) {
	ts, ok := ParseRowTime("8/13/2026", now)
	if !ok || ts.Day() != 13 || ts.Month() != time.August {
		t.Fatalf("want 13 August, got %v", ts)
	}
}

// An ambiguous date must follow the machine's own setting - this is the case
// that silently reorders a mailbox by months when it is guessed wrong.
func TestAmbiguousDateFollowsMachineOrder(t *testing.T) {
	t.Cleanup(func() { SetDayFirstForTest(nil) })

	yes, no := true, false

	SetDayFirstForTest(&yes)
	ts, ok := ParseRowTime("10/8/2026", now)
	if !ok || ts.Day() != 10 || ts.Month() != time.August {
		t.Fatalf("day-first machine: want 10 August, got %v", ts)
	}

	SetDayFirstForTest(&no)
	ts, ok = ParseRowTime("10/8/2026", now)
	if !ok || ts.Day() != 8 || ts.Month() != time.October {
		t.Fatalf("month-first machine: want 8 October, got %v", ts)
	}
}

func TestUnparseableLabelIsRefusedNotGuessed(t *testing.T) {
	for _, label := range []string{"", "Re: your ticket", "SOC Alerts"} {
		if ts, ok := ParseRowTime(label, now); ok {
			t.Fatalf("invented a timestamp for %q: %v", label, ts)
		}
	}
}

// --- message headers must be recognised whatever the clock ----------------

func TestBothClocksMarkAMessageHeader(t *testing.T) {
	for _, s := range []string{
		"AS SOC ALERTS<alerts@example.com> 14:35",
		"AS SOC ALERTS<alerts@example.com> 2:35 PM",
	} {
		if !MsgTimeRE.MatchString(s) {
			t.Fatalf("not recognised as a message header: %q", s)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
