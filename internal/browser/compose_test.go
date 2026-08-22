package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
)

// This is the only code in the project that puts a NEW mail into the world.
// Everything else replies to something that already exists, where the worst
// case is a wrong reply on a real thread. Here the worst case is mail to a
// stranger, so the refusals matter more than the happy path.

func TestComposeRefusesWithNoRecipient(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	mail := DefaultTestMail("", "", "610529")

	err := SendTestMail(page, mail, nil)
	if err == nil {
		t.Fatal("composed a mail with an empty To field")
	}
	if !strings.Contains(err.Error(), "no recipient") {
		t.Errorf("refused, but not for the right reason: %v", err)
	}
	// and it must refuse before opening anything
	if len(page.Find("#compose")) > 0 && composeIsVisible(page) {
		t.Error("the compose form was opened despite there being no recipient")
	}
}

// Whitespace is not a recipient.
func TestComposeRefusesAWhitespaceRecipient(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	if err := SendTestMail(page, DefaultTestMail("   ", "", "610529"), nil); err == nil {
		t.Fatal("accepted whitespace as an address")
	}
}

// The subject has to carry the case number in the shape a real alert uses, or
// the search being tested is not the search that runs in production.
func TestTestMailSubjectIsFindableByTheRealSearch(t *testing.T) {
	mail := DefaultTestMail("someone@example.com", "", "SOC610529")

	if mail.Case != "610529" {
		t.Errorf("Case = %q, want the bare number", mail.Case)
	}
	if !core.LabelMatchesCase(mail.Subject, "610529") {
		t.Errorf("the tool's own matcher cannot find the case in %q", mail.Subject)
	}
	// the first search the tool runs must appear in the subject
	first := core.BuildQueries("610529")[0]
	if !strings.Contains(mail.Subject, first) {
		t.Errorf("subject %q does not contain the first search term %q",
			mail.Subject, first)
	}
	if !strings.Contains(strings.ToUpper(mail.Subject), "TEST") {
		t.Errorf("subject %q does not say it is a test - it lands in a real "+
			"inbox and must be obvious", mail.Subject)
	}
}

// It must not name anyone's employer or look like a real alert: this is a
// message that arrives unannounced in someone's inbox.
func TestTestMailReadsAsATestAndNamesNoOrganisation(t *testing.T) {
	mail := DefaultTestMail("someone@example.com", "", "610529")
	body := strings.ToLower(mail.Body)

	if !strings.Contains(body, "ignore") {
		t.Error("the body never tells the reader to ignore it")
	}
	if !strings.Contains(body, "automated test") {
		t.Error("the body never says it is an automated test")
	}
	for _, word := range []string{"alert:", "urgent", "incident response", "severity"} {
		if strings.Contains(body, word) {
			t.Errorf("the body reads like a real alert (%q)", word)
		}
	}
}

// The body has to carry the auto-send phrase, or a run against this mail
// cannot exercise the auto-send path at all.
func TestTestMailCarriesTheAutoSendPhrase(t *testing.T) {
	mail := DefaultTestMail("someone@example.com", "", "610529")
	if !strings.Contains(strings.ToLower(mail.Body), "follow up") {
		t.Error("the body lacks the default auto-send phrase, so an " +
			"end-to-end run cannot test that path")
	}
}

// The happy path, against the stand-in mailbox: it fills the form and sends.
func TestComposeSendsAgainstTheStandInMailbox(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	mail := DefaultTestMail("someone@example.com", "cc@example.com", "610529")

	var logged []string
	log := Logf(func(format string, args ...any) {
		logged = append(logged, format)
	})

	if err := SendTestMail(page, mail, log); err != nil {
		t.Fatalf("could not send: %v\nlog: %v", err, logged)
	}
	page.Sleep(500 * time.Millisecond)

	if composeIsVisible(page) {
		t.Error("the compose form is still open, so nothing was sent")
	}
}

func composeIsVisible(p *Page) bool {
	var visible bool
	_ = p.eval(3*time.Second, `(function () {
      var c = document.getElementById("compose");
      return !!(c && c.style.display !== "none");
    })()`, &visible)
	return visible
}

// The search box once kept its previous contents, so the tool searched
// "SOC610529SOC700001" - which matched the OLD case and replied on the wrong
// thread. The read-back guard exists to catch exactly that.
//
// Teaching it about contenteditable fields nearly undid it: stale text
// CONTAINS the new text, so a containment check would have waved this through.
// Plain inputs must stay exact.
func TestStaleTextInAPlainFieldIsStillCaught(t *testing.T) {
	cases := []struct {
		name       string
		got, want  string
		rich, pass bool
	}{
		// A plain input is checked exactly. This is the guard that matters.
		{"the original bug", "SOC610529SOC700001", "SOC700001", false, false},
		{"exact, plain", "SOC700001", "SOC700001", false, true},
		{"leftovers, plain", "SOC700001 and more", "SOC700001", false, false},
		{"empty, plain", "", "SOC700001", false, false},

		// A rich field can only be checked for having taken something: once
		// OWA commits an address it shows a display name that may share
		// nothing with what was typed.
		{"an address chip", "Sam Okonkwo", "s.okonkwo@example.com", true, true},
		{"trailing newline, rich", "someone@example.com\n", "someone@example.com", true, true},
		{"empty, rich", "", "someone@example.com", true, false},
	}
	for _, c := range cases {
		if got := fieldHolds(c.got, c.want, c.rich); got != c.pass {
			t.Errorf("%s: fieldHolds(%q, %q, rich=%v) = %v, want %v",
				c.name, c.got, c.want, c.rich, got, c.pass)
		}
	}
}

// The search box is a plain input, and must stay one. If OWA ever ships it as
// a contenteditable, the exact check silently drops to "not empty" and the
// stale-text bug comes back unnoticed - so this asserts the assumption rather
// than trusting it.
func TestTheSearchBoxIsAPlainInput(t *testing.T) {
	page := servePage(t, fakeowa.Page)
	box := GetSearchBox(page)
	if box == nil {
		t.Fatal("no search box")
	}
	if err := page.Type(box, "SOC610529"); err != nil {
		t.Fatalf("could not type: %v", err)
	}
	if _, rich := page.fieldValue(box); rich {
		t.Error("the search box is a contenteditable, so its read-back check " +
			"has quietly weakened to 'not empty' - the stale-text guard is gone")
	}
}
