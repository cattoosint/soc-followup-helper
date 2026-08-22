package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
)

// A search that finds nothing has several very different causes that look
// identical from the outside. The diagnostic exists to tell them apart, so
// each cause is reproduced here and the report has to name the right one.
//
// These are the causes worth telling apart:
//
//   - the mailbox genuinely has no such mail
//   - this OWA build lays the message list out differently, so no selector
//     matches rows that are plainly on screen
//   - rows are found, but their timestamps are in a format that cannot be
//     read - which silently drops them from the last-resort selector and
//     breaks newest-first ordering
//
// Getting the wrong one costs a round trip to whoever owns the mailbox.

// servePage serves one mutated copy of the stand-in mailbox.
func servePage(t *testing.T, html string) *Page {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
	t.Cleanup(srv.Close)

	profile, err := os.MkdirTemp("", "socfu_diag_")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(profile) })

	page, err := Launch(context.Background(), Options{
		ProfileDir: profile,
		URL:        srv.URL,
		Headless:   true,
	})
	if err != nil {
		t.Skipf("could not drive a browser here: %v", err)
	}
	t.Cleanup(page.Close)
	page.Sleep(500 * time.Millisecond)
	return page
}

func mustSay(t *testing.T, report, want, why string) {
	t.Helper()
	if !strings.Contains(report, want) {
		t.Errorf("the report never says %q - %s\n\n%s", want, why, report)
	}
}

func mustNotSay(t *testing.T, report, unwanted, why string) {
	t.Helper()
	if strings.Contains(report, unwanted) {
		t.Errorf("the report says %q, but %s\n\n%s", unwanted, why, report)
	}
}

// A working mailbox must be reported as working - otherwise the diagnostic
// would send someone chasing a fault that is not there.
func TestDiagnoseSaysWhenTheSearchWorks(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	report := Diagnose(page, "610529", 300*time.Millisecond)

	mustSay(t, report, "this search works", "the mailbox does hold SOC610529")
	mustSay(t, report, "Search box  : found", "the search box is right there")
	mustNotSay(t, report, "FAULT", "nothing is wrong with this mailbox")
}

// The mailbox is fine, the tool is fine, the mail simply is not there. This
// must not be reported as a defect.
func TestDiagnoseSaysWhenTheMailboxSimplyHasNoSuchMail(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	report := Diagnose(page, "999999", 300*time.Millisecond)

	mustSay(t, report, "no results",
		"Outlook says so itself, and that is the honest answer")
	mustSay(t, report, "not a fault in the tool",
		"a missing mail is not a bug, and the report must say so plainly")
}

// An OWA build that labels its message list differently: the rows are on
// screen, but every selector misses them. Falling back to "no mail found"
// here would be a lie.
func TestDiagnoseSpotsAMessageListItCannotSee(t *testing.T) {
	html := fakeowa.Page
	html = strings.ReplaceAll(html, `<div id="list" aria-label="Message list" role="listbox"></div>`,
		`<div id="list" aria-label="Posteingang" role="grid"></div>`)
	html = strings.ReplaceAll(html, `role="option" data-convid=`, `role="row" data-convid=`)
	// and make sure Outlook is not claiming "no results" either
	html = strings.ReplaceAll(html, "We couldn't find anything.", "")

	page := servePage(t, html)
	report := Diagnose(page, "610529", 300*time.Millisecond)

	mustSay(t, report, "no rows matched ANY selector",
		"the rows are on screen but every selector missed them")
	mustSay(t, report, "laying out the message list differently",
		"that is the actual cause, and it must be named")
	mustNotSay(t, report, "not a fault in the tool",
		"this one IS a fault in the tool")
}

// Rows found, case number present, but the timestamps are in a format
// ParseRowTime cannot read. This is the quiet one: rows vanish from the
// last-resort selector and newest-first ordering has nothing to sort on.
func TestDiagnoseSpotsUnreadableTimestamps(t *testing.T) {
	html := fakeowa.Page
	// only the last-resort selector can match: no "Message list" label, no
	// data-convid, no MessageList section
	html = strings.ReplaceAll(html, `<div id="list" aria-label="Message list" role="listbox"></div>`,
		`<div id="list" role="listbox"></div>`)
	html = strings.ReplaceAll(html, `role="option" data-convid="' + m.id + '"`, `role="option"`)
	// A date with no colon and no slash. Note what does NOT break it: a
	// 24-hour clock ("13:10") and a dotted or dashed date next to a time both
	// still parse, because the time alone is enough. It takes a date-only row
	// written in words to leave nothing readable behind.
	html = strings.ReplaceAll(html, "Wed 8/5/2026 1:10 PM", "Wed 05 Aug 2026")
	html = strings.ReplaceAll(html, "Fri 8/7/2026 6:30 PM", "Fri 07 Aug 2026")

	page := servePage(t, html)
	report := Diagnose(page, "610529", 300*time.Millisecond)

	// This page reaches only the last-resort selector, which DROPS undated
	// rows - so the production search finds nothing at all here. The report
	// used to say "the search is fine; the ORDER is not", which is the
	// opposite of what RunSearch does, and this test asserted that wording and
	// locked it in.
	mustSay(t, report, "DROPS undated rows",
		"undated rows are dropped by this selector, so the real search finds "+
			"nothing here")
	mustSay(t, report, "finds nothing at all here",
		"the report must say the production search returns nothing")
	mustSay(t, report, "date format in the row",
		"the report has to point at the format, not just say it failed")
	mustNotSay(t, report, "this search works",
		"a matching row with no readable timestamp is not a working search")
	mustNotSay(t, report, "The search is fine",
		"the search is NOT fine: it returns nothing on this page")
}

// Without a search box nothing downstream can work, and saying anything about
// results would be guesswork.
func TestDiagnoseStopsWhenThereIsNoSearchBox(t *testing.T) {
	html := strings.ReplaceAll(fakeowa.Page, `id="topSearchInput"`, `id="notTheSearchBox"`)
	html = strings.ReplaceAll(html, `role="searchbox"`, `role="none"`)
	html = strings.ReplaceAll(html, `aria-label="Search"`, `aria-label="nope"`)
	html = strings.ReplaceAll(html, `placeholder="Search"`, `placeholder="nope"`)

	page := servePage(t, html)
	report := Diagnose(page, "610529", 300*time.Millisecond)

	mustSay(t, report, "the search box was not found",
		"nothing downstream can work without it")
	mustSay(t, report, "search-box selectors do not cover",
		"the report should list what it looked for")
}

// The report is read by whoever has the mailbox, and may be mailed back. It
// must carry row labels and nothing more - no message bodies.
func TestDiagnoseReportsRowLabelsOnly(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	report := Diagnose(page, "610529", 300*time.Millisecond)

	for _, secret := range []string{
		"alerts@example.com", // sender addresses live in message bodies
		"Please follow up",   // body text
	} {
		if strings.Contains(report, secret) {
			t.Errorf("the report leaked message content: %q\n\n%s", secret, report)
		}
	}
}
