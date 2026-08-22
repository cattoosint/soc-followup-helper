package browser

import (
	"fmt"
	"strings"
	"time"
)

// Composing a new mail exists for one reason: to give an end-to-end test
// something real to find. Without it, proving the tool against a real mailbox
// means waiting for a genuine SOC alert to arrive.
//
// This is the only code in the project that puts a NEW mail into the world
// rather than replying to one, so it is deliberately hard to trigger by
// accident: it is reachable only from --send-test-mail, it refuses to run
// without an explicit recipient, and the caller confirms the address first.

var newMailSelectors = []string{
	"button[aria-label='New mail']",
	"[aria-label='New mail']",
	"button[aria-label*='New mail' i]",
	"button[aria-label*='New message' i]",
	"[data-testid='newMailButton']",
	"button[title*='New mail' i]",
}

var toFieldSelectors = []string{
	"div[role='textbox'][aria-label*='To' i]",
	"input[aria-label*='To' i]",
	"[aria-label='To'] input",
	"div[aria-label*='To' i][contenteditable='true']",
	"input[id*='To']",
}

var ccFieldSelectors = []string{
	"div[role='textbox'][aria-label*='Cc' i]",
	"input[aria-label*='Cc' i]",
	"[aria-label='Cc'] input",
	"div[aria-label*='Cc' i][contenteditable='true']",
}

var ccToggleSelectors = []string{
	"button[aria-label*='Cc' i]",
	"[aria-label='Cc and Bcc']",
	"span[title='Cc']",
}

var subjectFieldSelectors = []string{
	"input[aria-label='Subject']",
	"input[aria-label*='Subject' i]",
	"[aria-label*='Add a subject' i]",
	"input[id*='subject' i]",
}

var bodyFieldSelectors = []string{
	"div[role='textbox'][aria-label*='essage body' i]",
	"div[role='textbox'][aria-label*='Body' i]",
	"div[contenteditable='true'][aria-label*='essage' i]",
}

// TestMail is one mail to compose.
type TestMail struct {
	To      string
	Cc      string
	Case    string // case number, put in the subject where a real alert has it
	Subject string
	Body    string
}

// DefaultTestMail builds the mail the tool sends for an end-to-end check.
//
// The subject carries the case number in the same shape a real alert does, so
// the search being tested is the search that runs in production. Everything
// else says plainly that this is a test, because it lands in a real inbox.
func DefaultTestMail(to, cc, caseNum string) TestMail {
	caseNum = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(caseNum)), "SOC")
	return TestMail{
		To:      to,
		Cc:      cc,
		Case:    caseNum,
		Subject: fmt.Sprintf("SOC%s [TEST] follow-up automation check", caseNum),
		Body: "This is an automated test message. Please ignore it.\n\n" +
			"It was sent to give a follow-up tool something to find, search " +
			"for and reply to. It is not an alert and needs no action.\n\n" +
			"follow up\n",
	}
}

// SendTestMail composes and sends one mail. It reports what it could not do
// rather than pressing on, because a half-filled compose form that gets sent
// is worse than one that never opens.
func SendTestMail(p *Page, mail TestMail, log Logf) error {
	if strings.TrimSpace(mail.To) == "" {
		return fmt.Errorf("no recipient: refusing to compose a mail with an " +
			"empty To field")
	}

	newMail := p.FindFirst(newMailSelectors...)
	if newMail == nil {
		return fmt.Errorf("could not find the New mail button - is the " +
			"mailbox open?")
	}
	if !p.Click(newMail) {
		return fmt.Errorf("could not click New mail")
	}
	p.Sleep(2 * time.Second)

	to := p.FindFirst(toFieldSelectors...)
	if to == nil {
		return fmt.Errorf("the compose form opened but its To field was not " +
			"found - nothing has been sent")
	}
	if err := p.Type(to, mail.To); err != nil {
		return fmt.Errorf("could not fill in To: %w", err)
	}
	// commit the address chip, or OWA drops the half-typed recipient
	p.Sleep(700 * time.Millisecond)
	_ = p.PressEnter()
	p.Sleep(500 * time.Millisecond)
	log.say("To: %s", mail.To)

	if strings.TrimSpace(mail.Cc) != "" {
		if err := fillCc(p, mail.Cc, log); err != nil {
			// A missing Cc is not worth abandoning the run over, but it must
			// be said out loud: reply-all is only meaningfully tested when
			// there is more than one recipient.
			log.say("could not fill in Cc (%v) - reply-all will have only "+
				"one recipient to include", err)
		} else {
			log.say("Cc: %s", mail.Cc)
		}
	}

	subject := p.FindFirst(subjectFieldSelectors...)
	if subject == nil {
		return fmt.Errorf("the Subject field was not found - nothing has " +
			"been sent")
	}
	if err := p.Type(subject, mail.Subject); err != nil {
		return fmt.Errorf("could not fill in the subject: %w", err)
	}
	log.say("Subject: %s", mail.Subject)

	if body := p.FindFirst(bodyFieldSelectors...); body != nil {
		if err := p.Type(body, mail.Body); err != nil {
			log.say("could not fill in the body: %v", err)
		}
	} else {
		log.say("the message body was not found - sending without one")
	}

	p.Sleep(500 * time.Millisecond)
	// Passing the case number is not ceremony: ClickSend re-reads the draft's
	// subject and refuses if it is not this case. That guard was written for
	// replies, and it is worth just as much here - it is the last thing
	// standing between a half-filled form and a real inbox.
	if !ClickSend(p, mail.Case, log) {
		return fmt.Errorf("did not send - the draft is still open in the " +
			"browser, and nothing has left the mailbox")
	}

	// A click is not a send. OWA leaves the draft open when a typed recipient
	// never resolved into a chip, and reporting "Sent." for that costs a
	// diagnosis round trip to whoever owns the mailbox.
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		p.Sleep(500 * time.Millisecond)
		if !ComposeIsOpen(p) {
			return nil
		}
	}
	return fmt.Errorf("Send was clicked but the draft is still open - most " +
		"likely the address never resolved. Nothing has been sent")
}

func fillCc(p *Page, cc string, log Logf) error {
	field := p.FindFirst(ccFieldSelectors...)
	if field == nil {
		// OWA hides Cc behind a toggle until it is asked for
		if toggle := p.FindFirst(ccToggleSelectors...); toggle != nil {
			p.Click(toggle)
			p.Sleep(700 * time.Millisecond)
			field = p.FindFirst(ccFieldSelectors...)
		}
	}
	if field == nil {
		return fmt.Errorf("the Cc field was not found")
	}
	if err := p.Type(field, cc); err != nil {
		return err
	}
	p.Sleep(700 * time.Millisecond)
	_ = p.PressEnter()
	p.Sleep(500 * time.Millisecond)
	return nil
}
