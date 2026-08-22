package browser

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/cdp"
	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
)

// These drive the real browser code against a stand-in mailbox, so the
// selectors, the search, the newest-message rule and the send guard are all
// exercised for real - just with nothing that can leave the machine.

var (
	testPage   *Page
	testServer *httptest.Server
)

func TestMain(m *testing.M) {
	testServer = fakeowa.Start()

	profile, err := os.MkdirTemp("", "socfu_profile_")
	if err != nil {
		panic(err)
	}

	page, err := Launch(context.Background(), Options{
		ProfileDir: profile,
		URL:        testServer.URL,
		Headless:   true,
	})
	if err != nil {
		testServer.Close()
		os.RemoveAll(profile)
		// Exiting 0 here once turned a broken handshake into a green run: every
		// browser test skipped, the suite reported ok, and a wrong constant sat
		// undetected. Only a machine with no browser at all gets a pass.
		if _, findErr := cdp.FindChrome(); findErr != nil {
			os.Stderr.WriteString("no browser on this machine, skipping: " +
				findErr.Error() + "\n")
			os.Exit(0)
		}
		os.Stderr.WriteString("Chrome is installed but could not be driven: " +
			err.Error() + "\n")
		os.Exit(1)
	}
	testPage = page

	code := m.Run()

	page.Close()
	testServer.Close()
	os.RemoveAll(profile)
	os.Exit(code)
}

// reload puts the fake mailbox back to a known state between tests.
func reload(t *testing.T) {
	t.Helper()
	if err := testPage.Navigate(testServer.URL); err != nil {
		t.Fatalf("reloading the fake mailbox: %v", err)
	}
	testPage.Sleep(200 * time.Millisecond)
}

func quietLog(string, ...any) {}

func TestSearchBoxIsFound(t *testing.T) {
	reload(t)
	if GetSearchBox(testPage) == nil {
		t.Fatal("search box not found in the fake mailbox")
	}
}

// The mailbox returns both SOC610529 and SOC1610529 for this query - exactly
// the substring hazard the digit-boundary rule exists for.
func TestSearchIgnoresLongerCaseNumbers(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) != 1 {
		var labels []string
		for _, h := range hits {
			labels = append(labels, h.Label)
		}
		t.Fatalf("got %d hits, want 1: %v", len(hits), labels)
	}
	if !strings.Contains(hits[0].Label, "SOC610529 Suspicious login") {
		t.Fatalf("matched the wrong mail: %q", hits[0].Label)
	}
	if strings.Contains(hits[0].Label, "SOC1610529") {
		t.Fatal("matched SOC1610529, a different case")
	}
}

// Two mails carry the same case; the newer one is the one to reply to, and it
// is chosen by its timestamp rather than its position in the list.
func TestNewestByTimestampWins(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC700001", 300*time.Millisecond, "700001",
		quietLog)
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	ordered := NewestFirst(hits)
	if !strings.Contains(ordered[0].Label, "Newer thread") {
		t.Fatalf("newest first picked %q", ordered[0].Label)
	}
}

func TestOpeningAMailShowsThatCase(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	if !testPage.Click(&hits[0]) {
		t.Fatal("could not click the result row")
	}
	if !WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog) {
		t.Fatal("reading pane never showed case 610529")
	}
}

// The previous case's mail lingers on screen after a click; acting on it would
// reply to the wrong thread.
func TestReadingPaneRefusesTheWrongCase(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	if WaitForReadingPane(testPage, 2*time.Second, "700001", quietLog) {
		t.Fatal("accepted a pane showing a different case")
	}
}

// The thread's newest message is from Jordan Lee; the older one is from SOC
// Alerts. Auto-send must be judged on the newest.
func TestReadLastMessageTakesTheNewest(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	if !WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog) {
		t.Fatal("mail did not open")
	}
	header, body := ReadLastMessage(testPage, quietLog)
	if !strings.Contains(strings.ToLower(header), "jordan") {
		t.Fatalf("newest sender = %q, want Jordan Lee", header)
	}
	if !strings.Contains(body, "follow up") {
		t.Fatalf("body = %q, want it to mention follow up", body)
	}
}

// The newest message quotes an earlier one from Jordan Lee. The quoted From:
// must not pass as the sender.
func TestQuotedHeaderIsNotTheSender(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC700001", 300*time.Millisecond, "700001",
		quietLog)
	ordered := NewestFirst(hits)
	if len(ordered) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&ordered[0])
	if !WaitForReadingPane(testPage, 5*time.Second, "700001", quietLog) {
		t.Fatal("mail did not open")
	}
	header, body := ReadLastMessage(testPage, quietLog)
	if strings.Contains(strings.ToLower(header), "jordan") {
		t.Fatalf("a quoted From: passed as the sender: %q", header)
	}
	if !strings.Contains(strings.ToLower(header), "soc alerts") {
		t.Fatalf("sender = %q, want the real sender line", header)
	}

	// and the full auto-send decision, end to end through the browser
	send, why := core.ShouldAutoSend(header, body, "follow up",
		"jordan@example.com")
	if send {
		t.Fatalf("auto-send fired on a quoted sender: %s", why)
	}
	send, why = core.ShouldAutoSend(header, body, "follow up",
		"alerts@example.com")
	if !send {
		t.Fatalf("auto-send refused the genuine sender: %s", why)
	}
}

func TestReplyAllOpensADraftForThisCase(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	if !WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog) {
		t.Fatal("mail did not open")
	}
	if !ClickReplyAll(testPage) {
		t.Fatal("Reply all was not clicked")
	}
	testPage.Sleep(300 * time.Millisecond)
	if !ComposeIsOpen(testPage) {
		t.Fatal("no draft opened")
	}
	subject := ComposeSubject(testPage)
	if !core.LabelMatchesCase(subject, "610529") {
		t.Fatalf("draft subject = %q, want case 610529", subject)
	}
}

// The Send button is found by DOM order, so a draft left open from an earlier
// case could otherwise be the one that goes out.
func TestSendRefusesADraftForAnotherCase(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	if !WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog) {
		t.Fatal("mail did not open")
	}
	if !ClickReplyAll(testPage) {
		t.Fatal("Reply all was not clicked")
	}
	testPage.Sleep(300 * time.Millisecond)

	if ClickSend(testPage, "700001", quietLog) {
		t.Fatal("sent a draft belonging to another case")
	}
	if !ComposeIsOpen(testPage) {
		t.Fatal("the draft was closed even though the send was refused")
	}
	if !ClickSend(testPage, "610529", quietLog) {
		t.Fatal("refused to send the draft for this case")
	}
	testPage.Sleep(300 * time.Millisecond)
	if ComposeIsOpen(testPage) {
		t.Fatal("the draft is still open after sending")
	}
}

func TestSentItemsVerification(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	if !WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog) {
		t.Fatal("mail did not open")
	}
	if !ClickReplyAll(testPage) {
		t.Fatal("Reply all was not clicked")
	}
	testPage.Sleep(300 * time.Millisecond)
	if !ClickSend(testPage, "610529", quietLog) {
		t.Fatal("send refused")
	}
	testPage.Sleep(300 * time.Millisecond)

	if got := VerifySent(testPage, "610529", time.Time{}, quietLog); got != SentFound {
		t.Fatalf("VerifySent = %v, want SentFound", got)
	}
}

func TestSentWatcherSeesTheDraftClose(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC610529", 300*time.Millisecond, "610529",
		quietLog)
	if len(hits) == 0 {
		t.Fatal("no results to open")
	}
	testPage.Click(&hits[0])
	WaitForReadingPane(testPage, 5*time.Second, "610529", quietLog)
	ClickReplyAll(testPage)
	testPage.Sleep(300 * time.Millisecond)

	watch := NewSentWatcher(testPage)
	if watch() {
		t.Fatal("watcher fired while the draft was still open")
	}
	ClickSend(testPage, "610529", quietLog)
	testPage.Sleep(300 * time.Millisecond)

	// two consecutive closed polls, guarding against a UI re-render
	if watch() {
		t.Fatal("watcher fired on the first closed poll")
	}
	if !watch() {
		t.Fatal("watcher never noticed the draft close")
	}
}

func TestEmptySearchIsReportedNotGuessed(t *testing.T) {
	reload(t)
	hits := RunSearch(testPage, "SOC999999", 300*time.Millisecond, "999999",
		quietLog)
	if len(hits) != 0 {
		t.Fatalf("invented %d hits for a case with no mail", len(hits))
	}
}
