package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

// consoleUI had no tests, and it cost twice over: an audit found AskOrWatch
// leaking a stdin reader that ate the analyst's answer, and the fix for it
// silently did not apply - the helpers were added, Ask and AskOrWatch went on
// using the old code, and the build stayed green because Go does not complain
// about an unused method.

// within runs f and fails fast if it blocks.
//
// Every test here can deadlock if the reader regresses, and a deadlocked test
// does not fail - it hangs until Go's 10-minute panic, which stalls the whole
// suite and reads as "still running" rather than "broken". CLAUDE.md says a
// test that cannot fail is worse than no test; a test that cannot FINISH is
// the same problem wearing a different hat.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s blocked for %s - stdin has deadlocked", what, d)
	}
}

// newTestUI feeds input and then EOF, which is what a finished script or a
// closed console looks like.
func newTestUI(input string) *consoleUI {
	return &consoleUI{in: bufio.NewReader(strings.NewReader(input))}
}

// newOpenTestUI feeds input and then keeps stdin OPEN, which is what a console
// with a human at it looks like: no more answers yet, but more may come.
func newOpenTestUI(t *testing.T, input string) (*consoleUI, io.Writer) {
	t.Helper()
	pr, pw := io.Pipe()
	if input != "" {
		go func() { _, _ = io.WriteString(pw, input) }()
	}
	t.Cleanup(func() { _ = pw.Close(); _ = pr.Close() })
	return &consoleUI{in: bufio.NewReader(pr)}, pw
}

// The whole point of the shared reader: an answer typed for one question must
// reach the question that is actually waiting.
func TestAnAnswerIsNotEatenBetweenPrompts(t *testing.T) {
	u := newTestUI("s\nr\n")

	var first, second string
	within(t, 5*time.Second, "the first prompt", func() {
		first = u.AskOrWatch("first > ", []string{"", "s", "r", "q"}, "review", nil)
	})
	if first != "s" {
		t.Fatalf("first prompt got %q, want \"s\"", first)
	}
	within(t, 5*time.Second, "the second prompt", func() {
		second = u.Ask("second > ", []string{"", "s", "r"}, "verify")
	})
	if got := second; got != "r" {
		t.Errorf("second prompt got %q, want \"r\" - the answer was eaten by a "+
			"reader left over from the first prompt", got)
	}
}

// A closed stdin is "stop", never a blind yes. The verify prompt does not
// offer "q", so this used to fall through to "yes, it was sent" and paint a
// green row nobody had confirmed.
func TestClosedStdinIsAStopNotAYes(t *testing.T) {
	u := newTestUI("") // EOF immediately

	var got string
	within(t, 5*time.Second, "Ask on a closed stdin", func() {
		got = u.Ask("was it sent? > ", []string{"", "s", "r"}, "verify")
	})
	if got != "q" {
		t.Errorf("got %q, want \"q\" - a closed stdin must never read as yes", got)
	}
	if !u.StopRequested() {
		t.Error("stdin closed but the run was not asked to stop")
	}
}

// The watcher fires when the draft closes. A keystroke still in flight for
// that question must not answer the NEXT one - but an answer typed in reply to
// the next question must still get through.
func TestOnlyAWatcherAnsweredPromptDrainsAheadOfItself(t *testing.T) {
	u, w := newOpenTestUI(t, "")

	// Nothing typed: the draft closing is what answers this one.
	fired := func() bool { return true }
	if got := u.AskOrWatch("first > ", []string{"", "s", "q"}, "review", fired); got != "auto" {
		t.Fatalf("got %q, want \"auto\"", got)
	}
	if !u.lastWasAuto {
		t.Fatal("the watcher answered the prompt, but that was not recorded, " +
			"so the stale keystroke will never be drained")
	}

	// The analyst's keypress lands just after the watcher moved on. It was
	// meant for a question that has already gone.
	_, _ = io.WriteString(w, "s\n")
	deadline := time.Now().Add(2 * time.Second)
	for len(u.stdinLines()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(u.stdinLines()) == 0 {
		t.Fatal("the keystroke never arrived; this test cannot prove anything")
	}

	u.drainStdin()
	if n := len(u.stdinLines()); n != 0 {
		t.Errorf("%d stale answer(s) left queued - one of them would answer "+
			"\"was it sent?\" for a case nobody looked at", n)
	}
}

// Draining must NOT happen when the analyst answered normally, or a queued
// answer - piped input, or someone typing ahead - is thrown away.
func TestAQueuedAnswerSurvivesANormallyAnsweredPrompt(t *testing.T) {
	u := newTestUI("s\nr\n")

	if got := u.AskOrWatch("first > ", []string{"", "s", "q"}, "review", nil); got != "s" {
		t.Fatalf("got %q, want \"s\"", got)
	}
	got := make(chan string, 1)
	go func() { got <- u.Ask("second > ", []string{"", "s", "r"}, "verify") }()
	select {
	case a := <-got:
		if a != "r" {
			t.Errorf("got %q, want \"r\"", a)
		}
	case <-time.After(2 * time.Second):
		t.Error("the queued answer was drained away - a piped or typed-ahead " +
			"answer must still be honoured")
	}
}

// An unrecognised answer must not be read as "sent".
func TestAnUnrecognisedAnswerIsRefusedNotAccepted(t *testing.T) {
	u := newTestUI("maybe\ns\n")

	var got string
	within(t, 5*time.Second, "Ask after an unrecognised answer", func() {
		got = u.Ask("was it sent? > ", []string{"", "s", "r"}, "verify")
	})
	if got != "s" {
		t.Errorf("got %q, want \"s\" - \"maybe\" should be re-asked, never "+
			"taken as an answer", got)
	}
}

// The reader must be started once and shared, not re-created per prompt.
func TestStdinHasExactlyOneReader(t *testing.T) {
	u := newTestUI("a\nb\n")
	first := u.stdinLines()
	second := u.stdinLines()
	if first != second {
		t.Error("stdinLines handed out two different channels, so two " +
			"goroutines are reading the same bufio.Reader")
	}
}
