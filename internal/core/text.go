package core

import (
	"regexp"
	"strings"
)

// Markers that mean a block of text is an unsent draft rather than a real
// message. Auto-send must never judge a thread on a draft.
var DraftMarkers = []string{"[draft]", "this message hasn't been sent", "saved:"}

// MsgTimeRE marks a line as a message header: 12-hour, 24-hour, or a date.
// A mailbox set to a 24-hour clock used to have no recognisable message
// headers at all, so auto-send could never fire.
var MsgTimeRE = regexp.MustCompile(`(?i)\d{1,2}:\d{2}\s*[AP]\.?M\.?|\b\d{1,2}:\d{2}\b|\d{1,2}/\d{1,2}(?:/\d{2,4})?`)

// QuotedHeaderRE finds the start of a quoted header or a recipient list.
// Cc and Bcc are load-bearing: dropping them once let a quoted header
// through as if it were the real sender line.
var QuotedHeaderRE = regexp.MustCompile(`(?i)\b(from|sent|subject|to|cc|bcc)\s*:`)

// NormalizeText lowercases and strips spacing plus icon glyphs.
//
// Outlook breaks long addresses for wrapping, so a header can read
// "soc alerts@example.com" - matching has to ignore that, and the icon
// characters the UI mixes into the same text.
func NormalizeText(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, ch := range value {
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
			ch == '\v' || ch == '\f':
			continue
		case ch >= 0xE000 && ch <= 0xF8FF: // private-use icon glyphs
			continue
		case ch == 0x200B || ch == 0xFEFF || ch == 0x00A0: // zero-width / nbsp
			continue
		}
		if isUnicodeSpace(ch) {
			continue
		}
		b.WriteRune(ch)
	}
	return strings.ToLower(b.String())
}

// isUnicodeSpace mirrors Python's str.isspace() for the separators Outlook
// actually emits, beyond the ASCII ones handled above.
func isUnicodeSpace(ch rune) bool {
	switch ch {
	case 0x0085, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return ch >= 0x2000 && ch <= 0x200A
}

// SenderPart returns just the sender of a message, from its header text.
//
// OWA renders a header as "<sender> To:<recipients> <date> ..." and the
// element text can also carry body content, including quoted headers from an
// earlier mail ("From: SOC Alerts <alerts@...> Sent: ..."). Matching against
// any of that would let a forwarded or quoted message pass as if the expected
// sender had written it, so anything at or after the first recipient or
// quoted-header marker is dropped, and so is anything after the first
// timestamp - the sender always precedes both.
//
// Returns "" when no plausible sender line can be isolated; callers must
// treat that as "do not auto-send".
func SenderPart(header string) string {
	text := strings.Join(strings.Fields(header), " ")
	if text == "" {
		return ""
	}
	if cut := QuotedHeaderRE.FindStringIndex(text); cut != nil {
		if cut[0] == 0 {
			// starts with "From:" - a quoted header, not a real sender line
			return ""
		}
		text = text[:cut[0]]
	}
	if stamp := MsgTimeRE.FindStringIndex(text); stamp != nil {
		text = text[:stamp[0]]
	}
	text = strings.TrimSpace(text)
	if len(text) > 120 {
		text = text[:120]
	}
	return text
}

// LooksLikeDraft reports whether text carries an unsent-draft marker.
func LooksLikeDraft(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range DraftMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
