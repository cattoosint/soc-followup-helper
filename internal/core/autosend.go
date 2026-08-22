package core

import (
	"fmt"
	"strings"
)

// ShouldAutoSend decides whether a case may be replied to with no human
// check. Both conditions must hold, or the analyst handles it by hand.
//
// lastHeader is the newest message's sender line (see browser.ReadLastMessage).
// The sender may be given as an address or a display name - whichever the
// mailbox shows. Returns (sendIt, reasonForTheLog).
//
// Every failure path here is deliberate: auto-send puts mail in front of real
// recipients with no human check, so anything ambiguous hands the case back.
func ShouldAutoSend(lastHeader, body, keyword, expectedSender string) (bool, string) {
	if keyword == "" || expectedSender == "" {
		return false, "auto-send needs both a phrase and a sender configured"
	}
	if body == "" {
		return false, "could not read the message"
	}
	if lastHeader == "" {
		return false, "could not identify who sent the latest message"
	}

	// compare with spacing removed: Outlook wraps long addresses mid-word
	// and sprinkles icon characters through the same text
	// Checked AFTER normalisation, mirroring the sender guard below. A phrase
	// of only spaces or zero-width characters normalises to "", and
	// strings.Contains(x, "") is always true - which quietly reduced auto-send
	// to a single condition while the log still reported a match.
	normKeyword := NormalizeText(keyword)
	if len(normKeyword) < 3 {
		return false, "the configured phrase is too short to match safely " +
			"- use a distinctive phrase from the mail"
	}
	keywordOK := strings.Contains(NormalizeText(body), normKeyword)

	normSender := NormalizeText(expectedSender)
	if len(normSender) < 4 {
		return false, "the configured sender is too short to match safely " +
			"- use the full address or display name"
	}

	// judged on the latest message's sender line only - never the whole
	// quoted thread, where the expected sender may appear from earlier mails
	senderOK := strings.Contains(NormalizeText(lastHeader), normSender)

	if keywordOK && senderOK {
		return true, fmt.Sprintf("matched '%s'; latest mail is from '%s'",
			keyword, expectedSender)
	}

	var missing []string
	if !keywordOK {
		missing = append(missing, fmt.Sprintf("'%s' not in the message", keyword))
	}
	if !senderOK {
		shown := lastHeader
		if len(shown) > 70 {
			shown = shown[:70]
		}
		missing = append(missing, fmt.Sprintf(
			"latest mail is not from '%s' (saw '%s')", expectedSender, shown))
	}
	return false, strings.Join(missing, "; ")
}
