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
	"github.com/cattoosint/socfollowup-test/internal/xlsx"
)

// A whole run, end to end, against a stand-in mailbox: sheet in, browser
// driven, results and colour-coded review sheet out. Nothing can leave the
// machine, so the auto-send path is exercised for real.

// scriptedUI answers every prompt the same way, and records what it was told.
type scriptedUI struct {
	answer  string
	logs    []string
	updates map[string]string
}

func newScriptedUI(answer string) *scriptedUI {
	return &scriptedUI{answer: answer, updates: map[string]string{}}
}

func (u *scriptedUI) Log(msg string) { u.logs = append(u.logs, msg) }

func (u *scriptedUI) CaseUpdate(num, status, detail string) {
	u.updates[num] = status
}

func (u *scriptedUI) Ask(prompt string, choices []string, kind string) string {
	return u.answer
}

func (u *scriptedUI) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {
	return u.answer
}

func (u *scriptedUI) StopRequested() bool { return false }

func (u *scriptedUI) transcript() string { return strings.Join(u.logs, "\n") }

// writeCaseSheet builds the analyst's export.
func writeCaseSheet(t *testing.T, path string, cases []string) {
	t.Helper()
	doc := xlsx.Doc{SheetName: "Sheet1"}
	doc.Rows = append(doc.Rows, xlsx.Row{
		Values: []string{"Case ID", "Summary"}, Bold: true})
	for _, c := range cases {
		doc.Rows = append(doc.Rows, xlsx.Row{Values: []string{c, "alert " + c}})
	}
	if err := xlsx.Save(path, doc); err != nil {
		t.Fatal(err)
	}
}

func chromeAvailable(t *testing.T) {
	t.Helper()
	if os.Getenv("SOCFU_SKIP_BROWSER") != "" {
		t.Skip("browser tests disabled")
	}
}

// TestFullRun covers the three outcomes that matter, in one pass:
//
//   - 610529 - newest message really is from the configured sender, so it
//     auto-sends and is confirmed in Sent Items
//   - 700001 - newest message only QUOTES that sender, so auto-send must
//     refuse and hand the case back
//   - 999999 - no mail at all, so it is flagged rather than guessed at
func TestFullRun(t *testing.T) {
	chromeAvailable(t)

	server := fakeowa.Start()
	defer server.Close()

	dir := t.TempDir()
	sheet := filepath.Join(dir, "shift.xlsx")
	writeCaseSheet(t, sheet, []string{"610529", "700001", "999999"})

	column, cases, err := core.ExtractCases(sheet, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("read %v from the sheet, want 3 cases", cases)
	}

	ui := newScriptedUI("s") // the analyst skips anything handed back
	toUI := func(format string, args ...any) {
		ui.Log("      [browser] " + fmt.Sprintf(format, args...))
	}
	summary := Run(context.Background(), sheet, column, cases, Options{
		URL:         server.URL,
		ProfileDir:  filepath.Join(dir, "profile"),
		OutputDir:   dir,
		Settle:      300 * time.Millisecond,
		SendDelay:   0,
		AutoSend:    true,
		AutoKeyword: "follow up",
		AutoSender:  "jordan@example.com",
		NoPause:     true,
		Headless:    true,
	}, ui, toUI)

	if summary.RunError != nil {
		t.Fatalf("run failed: %v\n%s", summary.RunError, ui.transcript())
	}
	if len(summary.Results) != 3 {
		t.Fatalf("got %d results, want 3:\n%s", len(summary.Results),
			ui.transcript())
	}

	got := map[string]Result{}
	for _, r := range summary.Results {
		got[r.Case] = r
	}

	// 1. the genuine sender: auto-sent and confirmed
	sent := got["SOC610529"]
	if sent.Status != core.StatusSent {
		t.Errorf("610529 = %s (%s), want SENT\n%s",
			sent.Status, sent.Detail, ui.transcript())
	}
	if !strings.Contains(sent.Reason, "confirmed in Sent Items") {
		t.Errorf("610529 reason = %q, want it to say it was confirmed",
			sent.Reason)
	}

	// 2. the quoted sender: refused, handed back, and the reason says why
	quoted := got["SOC700001"]
	if quoted.Status != core.StatusSkipped {
		t.Errorf("700001 = %s (%s), want SKIPPED - auto-send must not fire on "+
			"a quoted sender\n%s", quoted.Status, quoted.Detail, ui.transcript())
	}
	if !strings.Contains(quoted.Reason, "auto-send did not apply") {
		t.Errorf("700001 reason = %q, want it to explain why auto-send did not "+
			"apply", quoted.Reason)
	}

	// 3. no mail at all: flagged, and it says how hard it looked
	missing := got["SOC999999"]
	if missing.Status != core.StatusNotFound {
		t.Errorf("999999 = %s, want NOT_FOUND", missing.Status)
	}
	if !strings.Contains(missing.Reason, "Searched 3 ways (SOC999999, SOC 999999, 999999)") {
		t.Errorf("999999 reason = %q, want it to say how it searched",
			missing.Reason)
	}

	if summary.Replied != 1 || len(summary.NotFound) != 1 ||
		len(summary.Flagged) != 1 {
		t.Errorf("summary: %d replied, %d not found, %d flagged; want 1/1/1",
			summary.Replied, len(summary.NotFound), len(summary.Flagged))
	}

	// --- the files an analyst actually reads --------------------------------

	if summary.CSVPath == "" {
		t.Fatal("no results CSV was written")
	}
	raw, err := os.ReadFile(summary.CSVPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"SOC610529", "SOC700001", "SOC999999",
		"case,status,reason,detail"} {
		if !strings.Contains(body, want) {
			t.Errorf("results CSV is missing %q", want)
		}
	}

	if summary.XLSXPath == "" {
		t.Fatal("no review sheet was written")
	}
	book, err := xlsx.Open(summary.XLSXPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := book.Active.Rows
	if len(rows) != 4 {
		t.Fatalf("review sheet has %d rows, want 4", len(rows))
	}
	fills, err := xlsx.RowFills(summary.XLSXPath)
	if err != nil {
		t.Fatal(err)
	}

	wantColour := map[string]string{
		"610529": "C6EFCE", // green - replied
		"700001": "FFEB9C", // amber - skipped, still needs a human
		"999999": "FFC7CE", // red   - not found
	}
	for i, row := range rows[1:] {
		caseNum := row[0]
		if fills[i+1] != wantColour[caseNum] {
			t.Errorf("case %s painted %s, want %s",
				caseNum, fills[i+1], wantColour[caseNum])
		}
		if why := row[len(row)-1]; strings.TrimSpace(why) == "" {
			t.Errorf("case %s has no explanation in the Why column", caseNum)
		}
	}
}

// TestResultsSurviveAFailedLaunch proves an hour of a night shift is not lost
// when the browser cannot start.
func TestResultsSurviveAFailedLaunch(t *testing.T) {
	dir := t.TempDir()
	sheet := filepath.Join(dir, "shift.xlsx")
	writeCaseSheet(t, sheet, []string{"610529"})

	ui := newScriptedUI("")
	summary := Run(context.Background(), sheet, "Case ID", []string{"610529"},
		Options{
			URL:        "http://127.0.0.1:1",
			ProfileDir: filepath.Join(dir, "profile"),
			OutputDir:  dir,
			Headless:   true,
			// a path that cannot be a browser, so the launch fails outright
			ChromePath: filepath.Join(dir, "not-a-browser.exe"),
		}, ui, func(string, ...any) {})

	if summary.RunError == nil {
		t.Fatal("expected the run to report a launch failure")
	}
	if summary.CSVPath == "" {
		t.Error("no results file was written after the failure")
	}
	if summary.XLSXPath == "" {
		t.Error("no review sheet was written after the failure")
	}
	if !strings.Contains(ui.transcript(), "Run stopped") {
		t.Errorf("the failure was not reported to the analyst:\n%s",
			ui.transcript())
	}
}
