// Package engine runs the follow-up over a list of cases.
//
// It never imports the front-end. Everything the analyst sees goes through the
// UI interface, so the console, the web page and the tests can each supply
// their own without the engine knowing which it has.
package engine

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/browser"
	"github.com/cattoosint/socfollowup-test/internal/core"
)

// UI is everything the engine needs from a front-end.
type UI interface {
	// Log adds a line to the running commentary.
	Log(msg string)
	// CaseUpdate drives the tracker: one row per case.
	CaseUpdate(num, status, detail string)
	// Ask blocks until the analyst picks one of choices.
	Ask(prompt string, choices []string, kind string) string
	// AskOrWatch is Ask, but returns "auto" as soon as watch reports true.
	AskOrWatch(prompt string, choices []string, kind string, watch func() bool) string
	// StopRequested is checked between cases.
	StopRequested() bool
}

// Options are the run settings.
type Options struct {
	URL         string
	ProfileDir  string
	OutputDir   string
	Settle      time.Duration
	SendDelay   time.Duration
	AutoSend    bool
	AutoKeyword string
	AutoSender  string
	NoPause     bool
	Headless    bool
	// Unattended marks a run nobody is watching - the self-test. Such a run
	// always closes the browser afterwards: there is no analyst to read the
	// window, so leaving it up would only leak a process.
	Unattended bool
	ChromePath string
}

// Result is one case's outcome.
type Result struct {
	Case   string
	Status string
	Detail string
	Reason string
}

// Summary is what a finished run produced.
type Summary struct {
	Results  []Result
	CSVPath  string
	XLSXPath string
	Flagged  []string
	NotFound []string
	Errored  []string
	Replied  int
	Stopped  bool // ended before the last case: Stop, or a quit at a prompt
	RunError error
}

// NeedsALook reports whether the run left anything an analyst should see on
// screen: a case with no mail, a case that errored, or a run that stopped
// before working through every case.
func (s *Summary) NeedsALook() bool {
	return len(s.NotFound) > 0 || len(s.Errored) > 0 || s.Stopped
}

// Runner holds the browser for the length of a run.
type Runner struct {
	page *browser.Page
	opts Options
	ui   UI
	log  browser.Logf
	dir  string
}

// ProcessCase handles one case and reports (status, detail, reason).
//
// The order here is deliberate and load-bearing: the mailbox is confirmed, the
// mail is found and confirmed to be THIS case, the auto-send decision is taken
// while the original message is still what is on screen, and only then is
// anything clicked.
func (r *Runner) ProcessCase(num string) (string, string, string) {
	if !r.ensureMailbox() {
		return core.StatusError, "mailbox not reachable - sign in, then use Retry",
			"Outlook was not reachable for this case"
	}

	queries := core.BuildQueries(num)
	var items []browser.Element
	usedQuery := ""
	for _, q := range queries {
		items = browser.RunSearch(r.page, q, r.opts.Settle, num, r.log)
		if len(items) > 0 {
			usedQuery = q
			break
		}
		r.ui.Log(fmt.Sprintf("    no match for: %s", q))
	}

	if len(items) == 0 {
		r.shot("case_" + num + "_notfound")
		return core.StatusNotFound,
			fmt.Sprintf("no mail matched (tried %d searches)", len(queries)),
			fmt.Sprintf("Searched %d ways (%s) - no mail in this mailbox "+
				"mentions %s", len(queries), strings.Join(queries, ", "), num)
	}

	count := len(items)
	ordered := browser.NewestFirst(items)
	first := ordered[0]
	label := truncate(first.Label, 120)
	if count > 1 {
		r.ui.Log("    " + itoa(count) + " results - replying to the most recent one.")
	}
	r.log("opening: %s", label)
	if !r.page.Click(&first) {
		r.shot("case_" + num + "_open_fail")
		return core.StatusError, "could not open the matching mail",
			"Could not open the matching mail for case " + num
	}
	r.page.Sleep(time.Second)

	// must be showing THIS case before anything is read or clicked
	if !browser.WaitForReadingPane(r.page, 12*time.Second, num, r.log) {
		r.shot("case_" + num + "_not_opened")
		return core.StatusError, "mail for " + num + " did not open",
			"Found the mail but the reading pane never showed case " + num +
				" - nothing was clicked"
	}

	// decide about auto-send BEFORE replying, while the original mail is what
	// is on screen
	autoOK, autoWhy := false, "auto-send off"
	if r.opts.AutoSend {
		lastHeader, body := browser.ReadLastMessage(r.page, r.log)
		keyword := r.opts.AutoKeyword
		if keyword == "" {
			keyword = "follow up"
		}
		autoOK, autoWhy = core.ShouldAutoSend(lastHeader, body, keyword,
			r.opts.AutoSender)
		decision := "manual"
		if autoOK {
			decision = "SEND"
		}
		r.log("auto-send check for %s: %s (%s)", num, decision, autoWhy)
	}

	if !browser.ClickReplyAll(r.page) {
		r.shot("case_" + num + "_replyall_fail")
		return core.StatusError, "Reply all button not found",
			"Mail opened but Reply all was not there, in the toolbar or the " +
				"... menu - reply to this one by hand"
	}

	if autoOK {
		return r.autoSend(num, label, autoWhy)
	}

	// Stamped before the analyst can possibly send: any reply for this case
	// already in Sent Items is older than this, and must not count as proof
	// that tonight's reply left.
	openedAt := time.Now()

	r.page.Sleep(time.Second)
	if !browser.ComposeIsOpen(r.page) {
		r.ui.Log("    (couldn't confirm the draft opened - check the browser)")
	}

	manualNote := ""
	if r.opts.AutoSend {
		manualNote = " (auto-send did not apply: " + autoWhy + ")"
	}

	r.ui.Log(fmt.Sprintf("    Match (%d result(s), query %s):", count, usedQuery))
	if label != "" {
		r.ui.Log("    " + label)
	}
	r.ui.Log("    Reply-all draft is open in the browser. Review, edit, hit Send.")
	r.ui.CaseUpdate(num, "REVIEW", label)

	choice := r.ui.AskOrWatch(
		"    [Enter]=sent, next case | s=skip | r=retry this case | q=quit > ",
		[]string{"", "s", "r", "q"}, "review",
		browser.NewSentWatcher(r.page))

	switch choice {
	case "s":
		return core.StatusSkipped, label,
			"Draft was opened; the analyst skipped it without sending" + manualNote
	case "r":
		return r.ProcessCase(num)
	case "q":
		return core.StatusQuit, "", "Analyst stopped the run at this case"
	case "auto":
		r.ui.Log("    Draft closed - checking Sent Items...")
	default:
		r.ui.Log("    Checking Sent Items...")
	}

	// openedAt, not the zero time. VerifySent only applies its age floor when
	// it is given one, so a zero here accepts ANY matching row in Sent Items -
	// including this case's reply from an earlier run or an earlier shift.
	// The draft-closed watcher fires on "sent OR discarded", so discarding a
	// draft would then be reported as a confirmed send and painted green.
	switch browser.VerifySent(r.page, num, openedAt, r.log) {
	case browser.SentFound:
		r.ui.Log("    Verified in Sent Items.")
		return core.StatusSent, label,
			"Analyst sent it; reply confirmed in Sent Items" + manualNote
	case browser.SentUnknown:
		return core.StatusSentUnverified, label + " (send not verified)",
			"Analyst sent it; the Sent Items check could not run" + manualNote
	}

	r.ui.Log("    !!! Nothing matching in Sent Items.")
	second := r.ui.Ask(
		"    Draft closed but I can't find it in Sent Items - was it sent? "+
			"[Enter]=yes | s=no, mark skipped | r=retry this case > ",
		[]string{"", "s", "r"}, "verify")
	switch second {
	case "s":
		return core.StatusSkipped, label + " (draft closed without sending)",
			"Draft was closed or discarded without being sent" + manualNote
	case "r":
		return r.ProcessCase(num)
	case "":
		// The analyst says they sent it, but the folder was read and the
		// reply was not in it. That is weaker evidence than "the check could
		// not run", so it cannot be greener than it: unconfirmed, not sent.
		return core.StatusSentUnverified, label + " (send not verified)",
			"Analyst says they sent it, but it was not found in Sent Items" +
				manualNote
	}
	// Anything else is not an answer. consoleUI returns "q" when stdin is
	// closed precisely so it is never read as a blind yes, and this prompt
	// does not offer "q" - so without this the tool would treat "nobody
	// answered" as "yes, it was sent" and paint the row green.
	return core.StatusSkipped, label + " (no answer)",
		"Nobody answered whether this was sent, so it is left for a human" +
			manualNote
}

// autoSend sends without a human check. Everything here is written to fail
// loudly rather than to claim success it cannot prove.
func (r *Runner) autoSend(num, label, why string) (string, string, string) {
	r.page.Sleep(1500 * time.Millisecond) // let the draft finish building
	r.ui.Log("    Auto-send: " + why)

	if !browser.ClickSend(r.page, num, r.log) {
		r.shot("case_" + num + "_send_fail")
		r.ui.Log("    Didn't send automatically - review this one by hand.")
		return core.StatusError, label + " (auto-send refused)",
			"Auto-send was due (" + why + ") but the draft did not match this " +
				"case, so nothing was sent - reply by hand"
	}

	sentAt := time.Now()
	r.page.Sleep(2500 * time.Millisecond)

	switch browser.VerifySent(r.page, num, sentAt, r.log) {
	case browser.SentMissing:
		r.ui.Log("    !!! Sent, but not found in Sent Items - check it.")
		return core.StatusError, label + " (auto-sent, unconfirmed)",
			"Auto-sent (" + why + "), but no matching reply appeared in Sent " +
				"Items - check it by hand"
	case browser.SentUnknown:
		// say so rather than claiming a check that never ran
		r.ui.Log("    Sent automatically (could not check Sent Items).")
		return core.StatusSent, label + " (auto-sent, not verified)",
			"Auto-sent (" + why + "); the Sent Items check could not run"
	}
	r.ui.Log("    Sent automatically and verified.")
	return core.StatusSent, label + " (auto-sent)",
		"Auto-sent (" + why + ") and confirmed in Sent Items"
}

func (r *Runner) ensureMailbox() bool {
	if browser.GetSearchBox(r.page) != nil {
		return true
	}
	_ = r.page.Navigate(r.opts.URL)
	if browser.WaitForMailbox(r.page, 15*time.Second, r.log, nil) != nil {
		return true
	}
	r.ui.Log("    Signed out? Sign in again in the automated browser window " +
		"(choose 'Yes' at 'Stay signed in?'). It waits for the inbox.")
	return browser.WaitForMailbox(r.page, 30*time.Minute, r.log, r.ui.Log) != nil
}

func (r *Runner) shot(name string) {
	if r.dir == "" {
		return
	}
	if path, err := r.page.Screenshot(filepath.Join(r.dir, "screenshots"), name); err == nil {
		r.log("screenshot: %s", path)
	}
}

// Run works through every case and writes the result files.
//
// An hour of a night shift can sit in these results, so a failure part way
// through must not throw them away: whatever was finished is still written and
// reported.
func Run(ctx context.Context, srcPath, column string, cases []string,
	opts Options, ui UI, logf browser.Logf) Summary {

	var summary Summary
	results := &summary.Results

	page, err := browser.Launch(ctx, browser.Options{
		ProfileDir: opts.ProfileDir,
		URL:        opts.URL,
		Headless:   opts.Headless,
		ChromePath: opts.ChromePath,
		Log:        logf,
	})
	if err != nil {
		summary.RunError = err
		ui.Log("    !!! Run stopped: " + err.Error())
		writeOutputs(srcPath, column, opts, ui, &summary)
		return summary
	}
	// The browser is closed only when the run finished with nothing left to
	// look at. Anything else - a stop, an error, a case with no mail - leaves
	// the window up, because that window is the evidence.
	defer func() {
		quiet := opts.Headless || opts.Unattended
		if quiet || (summary.RunError == nil && !summary.NeedsALook()) {
			if !quiet {
				ui.Log("Run finished cleanly - closing the browser.")
			}
			page.Close()
			return
		}
		ui.Log("Leaving the browser open so you can see what it was looking " +
			"at. Close it when you are done; the next run frees this profile " +
			"by itself.")
		page.Leave()
	}()

	r := &Runner{page: page, opts: opts, ui: ui, log: logf, dir: opts.OutputDir}

	ui.Log("If a login page appears, sign in manually (MFA included) - it " +
		"waits until the inbox is open.")
	if browser.WaitForMailbox(page, 30*time.Minute, logf, ui.Log) == nil {
		summary.RunError = fmt.Errorf("the inbox never opened - sign in, then run again")
		ui.Log("    !!! Run stopped: " + summary.RunError.Error())
		writeOutputs(srcPath, column, opts, ui, &summary)
		return summary
	}
	ui.Log("Mailbox ready.\n")

	for i, num := range cases {
		// auto-send can carry a whole run without ever showing a prompt, so
		// check for a stop request between cases or Stop would be unusable
		if ui.StopRequested() {
			ui.Log("    Stopping at user request.")
			summary.Stopped = true
			break
		}
		ui.Log(fmt.Sprintf("[%d/%d] SOC%s", i+1, len(cases), num))
		ui.CaseUpdate(num, "WORKING", "")

		status, detail, reason := r.ProcessCase(num)

		ui.CaseUpdate(num, status, detail)
		*results = append(*results, Result{
			Case: "SOC" + num, Status: status, Detail: detail, Reason: reason})

		switch status {
		case core.StatusNotFound:
			ui.Log("    !!! NOT FOUND - flagged: SOC" + num)
			if !opts.NoPause {
				if ui.Ask("    [Enter]=continue | q=quit > ",
					[]string{"", "q"}, "notfound") == "q" {
					goto finished
				}
			}
		case core.StatusError:
			ui.Log("    !!! ERROR: " + detail)
		case core.StatusQuit:
			ui.Log("    Stopping at user request.")
			summary.Stopped = true
			goto finished
		}

		if status == core.StatusSent && i < len(cases)-1 && opts.SendDelay > 0 {
			ui.Log(fmt.Sprintf("    Waiting %s before the next case...",
				opts.SendDelay))
			page.Sleep(opts.SendDelay)
		}
	}

finished:
	writeOutputs(srcPath, column, opts, ui, &summary)
	return summary
}

// writeOutputs is called on every path out of a run, including a failed one.
func writeOutputs(srcPath, column string, opts Options, ui UI, s *Summary) {
	for _, r := range s.Results {
		switch r.Status {
		case core.StatusSent:
			s.Replied++
		case core.StatusNotFound:
			s.NotFound = append(s.NotFound, r.Case)
			s.Flagged = append(s.Flagged, r.Case)
		case core.StatusError:
			s.Errored = append(s.Errored, r.Case)
			s.Flagged = append(s.Flagged, r.Case)
		}
	}

	dir := opts.OutputDir
	if dir == "" {
		dir = "."
	}
	_ = os.MkdirAll(dir, 0o755)
	stamp := time.Now().Format("20060102_150405")

	csvPath := filepath.Join(dir, "followup_results_"+stamp+".csv")
	if err := writeResultsCSV(csvPath, s.Results); err == nil {
		s.CSVPath = csvPath
	} else {
		ui.Log("(couldn't write the results file: " + err.Error() + ")")
	}

	ui.Log(fmt.Sprintf("\nDone. %d replied, %d not found, %d errored, "+
		"%d processed.", s.Replied, len(s.NotFound), len(s.Errored),
		len(s.Results)))
	if len(s.Flagged) > 0 {
		ui.Log("NEEDS MANUAL FOLLOW-UP:")
		for _, r := range s.Results {
			if r.Status == core.StatusNotFound || r.Status == core.StatusError {
				ui.Log("  " + r.Case + " (" + r.Status + ")")
			}
		}
	}

	if srcPath == "" || column == "" {
		return
	}

	// Colour-coded copy of the analyst's own sheet: green = replied,
	// red = still needs a human, yellow = never got to it.
	statusByCase := map[string]string{}
	reasonByCase := map[string]string{}
	for _, r := range s.Results {
		num := strings.TrimPrefix(r.Case, "SOC")
		// The status is decided where the evidence is, not sniffed back out
		// of a human-readable string here. A detail line that stopped saying
		// "not verified" would silently have turned every unconfirmed send
		// green.
		status := r.Status
		statusByCase[num] = status
		reasonByCase[num] = r.Reason
	}

	var stats core.Stats
	_, _, _ = core.ExtractCases(srcPath, column, &stats)

	xlsx := filepath.Join(dir, "followup_review_"+stamp+".xlsx")
	if err := core.WriteReviewSheet(srcPath, column, statusByCase, xlsx,
		stats.DuplicateLines, reasonByCase); err != nil {
		// never lose the run's results just because the copy failed
		ui.Log("(couldn't write the review sheet: " + err.Error() + ")")
		return
	}
	s.XLSXPath = xlsx
	ui.Log("Review sheet (green = replied, red = needs follow-up): " +
		filepath.Base(xlsx))
}

func writeResultsCSV(path string, results []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Excel needs the BOM to read UTF-8 rather than the local codepage
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"case", "status", "reason", "detail"}); err != nil {
		return err
	}
	for _, r := range results {
		if err := w.Write([]string{r.Case, r.Status, r.Reason, r.Detail}); err != nil {
			return err
		}
	}
	return w.Error()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
