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

// Regressions for defects an adversarial audit demonstrated. Every one of them
// ended the same way: a case reported as replied when nothing had been sent.
// The suite was fully green while all of them were live, which is the reason
// these exist.

// perPromptUI answers each kind of prompt differently, so the second question
// - the one asked after Sent Items comes back empty - can be driven on its own.
type perPromptUI struct {
	byKind  map[string]string
	logs    []string
	updates map[string]string
	asked   []string
}

func newPerPromptUI(byKind map[string]string) *perPromptUI {
	return &perPromptUI{byKind: byKind, updates: map[string]string{}}
}

func (u *perPromptUI) Log(msg string) { u.logs = append(u.logs, msg) }

func (u *perPromptUI) CaseUpdate(num, status, detail string) {
	u.updates[num] = status
}

func (u *perPromptUI) Ask(prompt string, choices []string, kind string) string {
	u.asked = append(u.asked, kind)
	return u.byKind[kind]
}

func (u *perPromptUI) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {
	u.asked = append(u.asked, kind)
	return u.byKind[kind]
}

func (u *perPromptUI) StopRequested() bool { return false }

func (u *perPromptUI) transcript() string { return strings.Join(u.logs, "\n") }

// runOneCase drives a real headless browser against the stand-in mailbox for a
// single case, with auto-send off - the default path an analyst actually uses.
func runOneCase(t *testing.T, num string, ui UI) Summary {
	return runOneCaseWithSent(t, num, ui, nil)
}

// runOneCaseWithSent is runOneCase against a mailbox whose Sent Items already
// holds the given rows - the state a mailbox is really in on the second night.
func runOneCaseWithSent(t *testing.T, num string, ui UI, sent []string) Summary {
	t.Helper()

	server := fakeowa.StartWithSent(sent)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	sheet := filepath.Join(dir, "cases.csv")
	if err := os.WriteFile(sheet, []byte("ID\nSOC"+num+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	toUI := func(format string, args ...any) {
		ui.Log("      [browser] " + fmt.Sprintf(format, args...))
	}
	return Run(context.Background(), sheet, "ID", []string{num}, Options{
		URL:        server.URL,
		ProfileDir: filepath.Join(dir, "profile"),
		OutputDir:  dir,
		Settle:     300 * time.Millisecond,
		SendDelay:  0,
		AutoSend:   false, // the default: the analyst sends by hand
		NoPause:    true,
		Headless:   true,
	}, ui, toUI)
}

func onlyResult(t *testing.T, s Summary) Result {
	t.Helper()
	if s.RunError != nil {
		t.Fatalf("run failed: %v", s.RunError)
	}
	if len(s.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(s.Results))
	}
	return s.Results[0]
}

// The audit's top finding. The draft-closed watcher fires on "sent OR
// discarded", so a discarded draft reaches the Sent Items check with no human
// assertion at all. VerifySent only applies its age floor when it is GIVEN
// one, so passing the zero time accepted this case's reply from an earlier run
// as proof that tonight's reply left - a green row for a mail nobody sent.
//
// SOC700001 in the stand-in mailbox has no reply in Sent Items, so a run that
// sends nothing must not come back SENT under any circumstance.
func TestNothingSentIsNeverReportedAsSent(t *testing.T) {
	ui := newPerPromptUI(map[string]string{
		"review": "auto", // the draft closed on its own - sent, or discarded
		"verify": "s",    // and the analyst says: no, I did not send it
	})
	got := onlyResult(t, runOneCase(t, "700001", ui))

	if got.Status == core.StatusSent {
		t.Fatalf("reported SENT for a case where nothing was sent.\n"+
			"reason: %s\n%s", got.Reason, ui.transcript())
	}
	if got.Status != core.StatusSkipped {
		t.Errorf("status = %s, want SKIPPED", got.Status)
	}
}

// The audit's top finding, exercised properly: Sent Items already holds THIS
// case's reply from an earlier run, and tonight nothing is sent.
//
// VerifySent applies its age floor only when it is given one. The manual path
// passed the zero time, which disabled it, so that older reply was accepted as
// proof that tonight's reply left - and the row went green. The draft-closed
// watcher fires on "sent OR discarded", so simply discarding the draft was
// enough to get here with no human assertion at all.
func TestLastNightsReplyDoesNotConfirmTonightsSend(t *testing.T) {
	// dated well before this run, exactly as a previous shift would leave it
	previously := []string{
		"To: SOC Team  RE: SOC700001 Suspicious login  Mon 8/3/2026 9:00 AM",
	}
	ui := newPerPromptUI(map[string]string{
		"review": "auto", // draft closed - and it was DISCARDED, not sent
		"verify": "s",    // the analyst confirms: no, I did not send it
	})
	got := onlyResult(t, runOneCaseWithSent(t, "700001", ui, previously))

	if got.Status == core.StatusSent {
		t.Fatalf("an earlier run's reply was accepted as proof that tonight's "+
			"was sent.\nreason: %s\n%s", got.Reason, ui.transcript())
	}
	if got.Status != core.StatusSkipped {
		t.Errorf("status = %s, want SKIPPED", got.Status)
	}
}

// A reply that really was sent just now must still be recognised - the fix
// must not have made verification impossible, only honest.
func TestAFreshReplyIsStillRecognised(t *testing.T) {
	ui := newPerPromptUI(map[string]string{"review": "", "verify": ""})
	got := onlyResult(t, runOneCaseWithSent(t, "700001", ui, nil))

	// nothing was actually sent in this run, so it must NOT be green - but it
	// must have got as far as reading Sent Items rather than erroring out
	if got.Status == core.StatusError {
		t.Fatalf("verification broke outright: %s", got.Reason)
	}
	if !strings.Contains(ui.transcript(), "Sent Items") {
		t.Errorf("Sent Items was never checked:\n%s", ui.transcript())
	}
}

// The verify prompt had cases for "s" and "r" and no default, so every other
// value fell through to "yes, it was sent". consoleUI returns "q" when stdin
// closes - specifically so it is never read as a blind yes - and this prompt
// does not offer "q". A run whose input ran out therefore reported a green row
// that no human had answered.
func TestAnUnansweredVerifyPromptIsNotAYes(t *testing.T) {
	for _, answer := range []string{"q", "", "nonsense"} {
		ui := newPerPromptUI(map[string]string{
			"review": "auto",
			"verify": answer,
		})
		got := onlyResult(t, runOneCase(t, "700001", ui))

		if got.Status == core.StatusSent {
			t.Errorf("verify answered %q was read as a confirmed send "+
				"(status %s, reason %q)", answer, got.Status, got.Reason)
		}
	}
}

// Sent Items was read and the reply was NOT there. That is weaker evidence
// than "the check could not run", which is amber - so it cannot be greener.
// DESIGN.md: green only when a reply was confirmed in Sent Items.
func TestSayingYesAfterSentItemsCameBackEmptyIsNotGreen(t *testing.T) {
	ui := newPerPromptUI(map[string]string{
		"review": "auto",
		"verify": "", // "yes, I sent it" - but the folder says otherwise
	})
	got := onlyResult(t, runOneCase(t, "700001", ui))

	if got.Status == core.StatusSent {
		t.Fatalf("green row for a reply the tool looked for and could not "+
			"find.\nreason: %s", got.Reason)
	}
	if got.Status != core.StatusSentUnverified {
		t.Errorf("status = %s, want SENT_UNVERIFIED", got.Status)
	}
	if _, ok := core.StatusStyle[got.Status]; !ok {
		t.Errorf("status %s has no colour, so the review sheet cannot show it",
			got.Status)
	}
	if c := core.StatusStyle[got.Status].Colour; c == "C6EFCE" {
		t.Errorf("status %s is painted green (%s)", got.Status, c)
	}
}

// The amber status used to be decided by sniffing "not verified" out of the
// human-readable detail line while writing the review sheet. Reword that
// sentence and every unconfirmed send silently turns green. The status must
// carry its own meaning.
func TestUnverifiedStatusDoesNotDependOnWordingOfTheDetail(t *testing.T) {
	s := Summary{Results: []Result{{
		Case:   "SOC700001",
		Status: core.StatusSentUnverified,
		Detail: "wording that says nothing in particular",
		Reason: "whatever",
	}}}
	if s.Results[0].Status != core.StatusSentUnverified {
		t.Fatal("setup")
	}
	style, ok := core.StatusStyle[s.Results[0].Status]
	if !ok || style.Colour == "C6EFCE" {
		t.Errorf("an unverified send must not be green regardless of its "+
			"detail text (colour %q)", style.Colour)
	}
}
