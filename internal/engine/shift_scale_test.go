package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
)

// A real night shift is 30-50 cases. Every test before this one ran three,
// which cannot show what a shift shows: the result list changing under the
// tool between cases, the previous case's mail lingering on screen, and every
// status landing in one review sheet together.

// shiftUI answers per case number, so one run can produce every outcome.
type shiftUI struct {
	byCase  map[string]string
	current string
	logs    []string
	updates map[string][]string
}

func (u *shiftUI) Log(msg string) { u.logs = append(u.logs, msg) }

func (u *shiftUI) CaseUpdate(num, status, detail string) {
	if u.updates == nil {
		u.updates = map[string][]string{}
	}
	u.current = num
	u.updates[num] = append(u.updates[num], status)
}

func (u *shiftUI) answer() string {
	if a, ok := u.byCase[u.current]; ok {
		return a
	}
	return "s"
}

func (u *shiftUI) Ask(prompt string, choices []string, kind string) string {
	return u.answer()
}

func (u *shiftUI) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {
	return u.answer()
}

func (u *shiftUI) StopRequested() bool { return false }

// A 30-case shift, end to end, against a mailbox holding 30 mails.
func TestAThirtyCaseShift(t *testing.T) {
	const total = 30

	var mails []fakeowa.InboxMail
	var cases []string
	var sheet strings.Builder
	sheet.WriteString("ID\n")

	missing := map[string]bool{"700007": true, "700018": true} // no mail for these

	for i := 1; i <= total; i++ {
		num := fmt.Sprintf("7000%02d", i)
		cases = append(cases, num)
		sheet.WriteString("SOC" + num + "\n")
		if missing[num] {
			continue
		}
		mails = append(mails, fakeowa.InboxMail{
			Case:    num,
			Subject: "Suspicious login",
			Sender:  "SOC Alerts <alerts@example.com>",
			// spread across the week so ordering has real work to do
			Stamp: fmt.Sprintf("Wed 8/%d/2026 %d:%02d PM", 5+(i%3), 1+(i%9), i%60),
			Body:  "Please follow up on this alert. Host quarantined.",
		})
	}

	server := fakeowa.StartWithInbox(mails, nil)
	defer server.Close()

	dir := t.TempDir()
	sheetPath := filepath.Join(dir, "shift.csv")
	if err := os.WriteFile(sheetPath, []byte(sheet.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// A mixed shift: most skipped, a few retried, one quit near the end.
	ui := &shiftUI{byCase: map[string]string{
		"700003": "r", // retry once, then skip
		"700011": "r",
	}}

	start := time.Now()
	summary := Run(context.Background(), sheetPath, "ID", cases, Options{
		URL:        server.URL,
		ProfileDir: filepath.Join(dir, "profile"),
		OutputDir:  dir,
		Settle:     150 * time.Millisecond,
		SendDelay:  0,
		AutoSend:   false,
		NoPause:    true,
		Headless:   true,
	}, ui, func(string, ...any) {})

	if summary.RunError != nil {
		t.Fatalf("the shift did not finish: %v", summary.RunError)
	}
	t.Logf("%d cases in %s", len(summary.Results), time.Since(start).Round(time.Second))

	// 1. every case in the sheet is accounted for, exactly once
	if len(summary.Results) != total {
		t.Fatalf("got %d results for %d cases - a case with no result is a "+
			"case nobody follows up on", len(summary.Results), total)
	}
	seen := map[string]bool{}
	for _, r := range summary.Results {
		num := strings.TrimPrefix(r.Case, "SOC")
		if seen[num] {
			t.Errorf("case %s appears twice in the results", num)
		}
		seen[num] = true
	}
	for _, num := range cases {
		if !seen[num] {
			t.Errorf("case %s is missing from the results entirely", num)
		}
	}

	// 2. nothing was sent, so nothing may be reported as replied
	if summary.Replied != 0 {
		t.Errorf("Replied = %d, but nothing was sent in this run", summary.Replied)
	}
	for _, r := range summary.Results {
		if r.Status == core.StatusSent {
			t.Errorf("%s reported SENT with nothing sent (reason %q)",
				r.Case, r.Reason)
		}
		if r.Reason == "" {
			t.Errorf("%s has status %s with no reason - a status nobody can "+
				"review is not a result", r.Case, r.Status)
		}
	}

	// 3. the cases with no mail are the ones flagged NOT_FOUND, and only those
	notFound := map[string]bool{}
	for _, r := range summary.Results {
		if r.Status == core.StatusNotFound {
			notFound[strings.TrimPrefix(r.Case, "SOC")] = true
		}
	}
	for num := range missing {
		if !notFound[num] {
			t.Errorf("case %s has no mail in the mailbox but was not flagged "+
				"NOT_FOUND", num)
		}
	}
	for num := range notFound {
		if !missing[num] {
			t.Errorf("case %s was flagged NOT_FOUND, but its mail is in the "+
				"mailbox - the analyst would chase a case that is fine", num)
		}
	}

	// 4. the review sheet exists and paints nothing green
	if summary.XLSXPath == "" {
		t.Fatal("no review sheet was written")
	}
	if _, err := os.Stat(summary.XLSXPath); err != nil {
		t.Fatalf("review sheet missing: %v", err)
	}
	for _, r := range summary.Results {
		style, ok := core.StatusStyle[r.Status]
		if !ok {
			t.Errorf("%s has status %q with no colour, so the review sheet "+
				"cannot show it", r.Case, r.Status)
			continue
		}
		if style.Colour == "C6EFCE" {
			t.Errorf("%s is painted green after a run that sent nothing", r.Case)
		}
	}

	// 5. every case reached the tracker
	for _, num := range cases {
		if len(ui.updates[num]) == 0 {
			t.Errorf("case %s never appeared on the tracker, so the analyst "+
				"watching would not know it had been handled", num)
		}
	}
}
