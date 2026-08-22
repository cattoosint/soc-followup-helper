package browser

import (
	"strings"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/core"
)

// Logf receives progress worth putting in the run log.
type Logf func(format string, args ...any)

func (l Logf) say(format string, args ...any) {
	if l != nil {
		l(format, args...)
	}
}

var searchBoxSelectors = []string{
	"#topSearchInput",
	"input[role='searchbox']",
	"[role='searchbox']",
	"input[aria-label*='Search' i]",
	"input[placeholder*='Search' i]",
}

// loginURLHints mark a Microsoft sign-in page rather than the mailbox.
var loginURLHints = []string{
	"login.microsoftonline.com", "login.live.com", "login.microsoft",
	"/oauth", "signin",
}

// MessageListSelectors are tried in order.
//
// The search box's autocomplete dropdown uses the same listbox/option roles as
// the message list, so a bare "[role='option']" also matches suggestions -
// clicking one runs a search instead of opening mail. Prefer the message list.
var MessageListSelectors = []string{
	"div[aria-label='Message list'] div[role='option']",
	"[aria-label*='Message list' i] [role='option']",
	"[data-app-section='MessageList'] [role='option']",
	"[role='listbox'][aria-label*='essage'] [role='option']",
	"div[role='listbox'] div[role='option'][data-convid]",
	"[role='listbox'] [role='option']", // last resort
}

// GetSearchBox returns the mailbox search field, or nil when it is not there.
func GetSearchBox(p *Page) *Element {
	return p.FindFirst(searchBoxSelectors...)
}

// OnLoginPage reports whether the tab is sitting on a sign-in page.
func OnLoginPage(p *Page) bool {
	url := strings.ToLower(p.CurrentURL())
	for _, hint := range loginURLHints {
		if strings.Contains(url, hint) {
			return true
		}
	}
	return p.FindFirst("input[type='password']", "input[name='loginfmt']",
		"#i0116") != nil
}

// WaitForMailbox waits until the inbox is actually open.
//
// Signing in can take a while - MFA, a password prompt, picking an account -
// so this keeps waiting and says what it is waiting for rather than failing
// while the analyst is still typing.
func WaitForMailbox(p *Page, timeout time.Duration, log Logf, notify func(string)) *Element {
	deadline := time.Now().Add(timeout)
	announced := false
	nextNudge := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if box := GetSearchBox(p); box != nil {
			if announced {
				log.say("signed in - inbox reached")
				if notify != nil {
					notify("    Signed in - inbox loaded, carrying on.")
				}
			}
			return box
		}
		if !announced && OnLoginPage(p) {
			announced = true
			log.say("sign-in page detected - waiting for the analyst")
			if notify != nil {
				notify("    Sign-in page - log in in the browser window " +
					"(tick 'Stay signed in?'). Waiting for the inbox...")
			}
		}
		if time.Now().After(nextNudge) {
			nextNudge = time.Now().Add(30 * time.Second)
			left := time.Until(deadline)
			log.say("still waiting for the inbox (%ds left)", int(left.Seconds()))
			if notify != nil && announced {
				notify("    Still waiting for sign-in... (" +
					itoa(int(left.Minutes())) + " min left)")
			}
		}
		p.Sleep(2 * time.Second)
	}
	log.say("gave up waiting for the inbox after %s", timeout)
	return nil
}

// VisibleResults returns the message rows currently on screen.
func VisibleResults(p *Page) []Element {
	last := len(MessageListSelectors) - 1
	for i, css := range MessageListSelectors {
		found := p.Find(css)
		if i == last && len(found) > 0 {
			// Generic selector: also matches autocomplete suggestions. Every
			// real mail row shows a date/time, suggestions do not - so require
			// one rather than risk clicking a suggestion.
			var dated []Element
			for _, e := range found {
				if _, ok := core.ParseRowTime(e.Label, time.Time{}); ok {
					dated = append(dated, e)
				}
			}
			found = dated
		}
		if len(found) > 0 {
			return found
		}
	}
	return nil
}

// DismissSuggestions closes the search autocomplete popup so it cannot be
// mistaken for results, and cannot swallow the next click.
func DismissSuggestions(p *Page) {
	popup := p.Find(
		"[role='listbox'][aria-label*='uggest' i]",
		"[role='listbox'] [role='option'][id*='uggest']",
	)
	if len(popup) > 0 {
		_ = p.PressEscape()
		p.Sleep(400 * time.Millisecond)
	}
}

// RunSearch searches OWA and returns the matching result rows, or nil.
//
// When num is given, only results whose row text actually contains that case
// number count as a match - so a search that silently returned the whole inbox
// is not mistaken for a hit.
func RunSearch(p *Page, query string, settle time.Duration, num string, log Logf) []Element {
	if !typeSearch(p, query, log) {
		return nil
	}
	// let autocomplete settle so Enter submits what we typed
	p.Sleep(400 * time.Millisecond)
	if err := p.PressEnter(); err != nil {
		log.say("could not submit the search: %v", err)
		return nil
	}
	p.Sleep(settle) // let the result list replace the inbox list
	DismissSuggestions(p)

	deadline := time.Now().Add(12 * time.Second)
	misses := 0
	for time.Now().Before(deadline) {
		items := VisibleResults(p)
		if len(items) > 0 {
			if num == "" {
				return items
			}
			hits := matching(items, num)
			if len(hits) > 0 {
				// let the list settle: results stream in, and returning on the
				// first poll can miss a newer mail arriving a moment later -
				// which is the one that should be replied to
				p.Sleep(800 * time.Millisecond)
				settled := matching(VisibleResults(p), num)
				if len(settled) > len(hits) {
					log.say("result list grew from %d to %d while settling",
						len(hits), len(settled))
					hits = settled
				}
				log.say("search %q -> %d row(s), %d match case %s",
					query, len(items), len(hits), num)
				return hits
			}
			misses++
			if misses >= 3 { // results are showing, none are this case
				log.say("search %q -> %d row(s), none contain %s",
					query, len(items), num)
				return nil
			}
		} else if core.NoResultsRE.MatchString(p.BodyText()) {
			log.say("search %q -> Outlook reports no results", query)
			return nil
		}
		p.Sleep(600 * time.Millisecond)
	}
	log.say("search %q -> nothing appeared within the timeout", query)
	return nil
}

// typeSearch puts query in the search box, retrying with a freshly located
// box each time.
//
// OWA re-creates the search input as the analyst types - it is a controlled
// component, and a re-render between the clear and the keystrokes leaves the
// text in a node that is no longer on the page. The read-back then reports the
// box as empty, or holding only the tail ("0020" of "SOC 700020"), and the
// search is skipped. Three of those in a row is a case reported NOT FOUND with
// the mail sitting in the mailbox - which is exactly what a live run did.
//
// The read-back itself stays strict: searching with the wrong text is how the
// previous case's number got searched for. This retries rather than relaxing.
func typeSearch(p *Page, query string, log Logf) bool {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		box := GetSearchBox(p)
		if box == nil {
			log.say("search box not found - are you logged in?")
			return false
		}
		if err := p.Type(box, query); err == nil {
			if attempt > 1 {
				log.say("search box took %q on attempt %d", query, attempt)
			}
			return true
		} else {
			lastErr = err
		}
		// let the re-render finish before locating the box again
		p.Sleep(700 * time.Millisecond)
	}
	log.say("could not type the search after 3 attempts: %v", lastErr)
	return false
}

func matching(items []Element, num string) []Element {
	var out []Element
	for _, e := range items {
		if core.LabelMatchesCase(e.Label, num) {
			out = append(out, e)
		}
	}
	return out
}

// NewestFirst orders rows newest-first by their displayed timestamp.
func NewestFirst(items []Element) []Element {
	timed := make([]core.Timed[Element], 0, len(items))
	for _, e := range items {
		timed = append(timed, core.Timed[Element]{Label: e.Label, Item: e})
	}
	return core.SortNewestFirst(timed, time.Time{})
}

// ClickReplyAll clicks Reply all - toolbar button first, then the "..."
// overflow menu.
//
// On narrow windows OWA hides Reply all behind the "..." (More actions) button
// next to the message header, so the direct button is not there.
func ClickReplyAll(p *Page) bool {
	direct := p.FindFirst(
		"button[aria-label='Reply all']",
		"[aria-label='Reply all']",
		"button[title='Reply all']",
		"[aria-label*='Reply all']",
	)
	if p.Click(direct) {
		return true
	}

	more := p.FindFirst(
		"button[aria-label='More options']",
		"button[aria-label='More actions']",
		"button[aria-label='More commands']",
		"[aria-label='More options']",
		"[aria-label*='More actions' i]",
		"[data-testid='ThreeDotButton']",
	)
	if !p.Click(more) {
		return false
	}
	p.Sleep(800 * time.Millisecond)

	if p.Click(p.FindByText("Reply all")) {
		return true
	}
	if p.Click(p.FindFirst("[aria-label='Reply all']")) {
		return true
	}

	// some builds tuck it under "Other reply actions"
	if p.Click(p.FindByText("Other reply actions")) {
		p.Sleep(800 * time.Millisecond)
		if p.Click(p.FindByText("Reply all")) {
			return true
		}
	}
	_ = p.PressEscape() // leave no menu hanging open for the next case
	return false
}

// ReadingPane returns the open message pane, or nil.
func ReadingPane(p *Page) *Element {
	return p.FindFirst("[aria-label*='Reading pane' i]",
		"#ReadingPaneContainerId", "div[role='document']")
}

// ReadLastMessage returns the header and body of the NEWEST message in the
// open conversation.
//
// header is that message's sender line - the one auto-send is judged on, so a
// thread whose latest reply came from someone else is never treated as if it
// came from the expected sender. Returns ("", "") when the page cannot be
// read, which callers must treat as "do not auto-send".
func ReadLastMessage(p *Page, log Logf) (string, string) {
	root := ReadingPane(p)
	if root == nil {
		return "", ""
	}
	body := p.TextOf(root)
	if body == "" {
		return "", ""
	}

	type candidate struct {
		y     float64
		text  string
		stamp time.Time
		dated bool
	}
	var candidates []candidate

	for _, el := range p.FindAllWithin(root,
		"div[aria-label*='message' i]", "div[role='listitem']") {
		text := strings.Join(strings.Fields(el.Text), " ")
		if text == "" || !core.MsgTimeRE.MatchString(text) {
			continue // not a message header
		}
		// Test the HEADER for draft markers, not the whole element. The text
		// carries the body and quoted history too, so matching across all of
		// it dropped real messages whose body merely contained "saved:" or
		// "[draft]" - and dropping the newest message hands the auto-send
		// decision to an older one. In a SOC thread the oldest message is the
		// original alert, which always matches the rule.
		header := text
		if loc := core.MsgTimeRE.FindStringIndex(header); loc != nil {
			header = header[:loc[1]]
		}
		if core.LooksLikeDraft(header) {
			continue // an unsent draft, not a real message
		}
		stamp, dated := core.ParseRowTime(text, time.Time{})
		candidates = append(candidates,
			candidate{y: el.Y, text: text, stamp: stamp, dated: dated})
	}

	if len(candidates) == 0 {
		return "", body
	}

	// Prefer the newest by its own timestamp. Screen position is not reliable:
	// "Show newest messages on top" is a per-mailbox setting, and under it the
	// bottom-most message is the OLDEST one.
	var timed []candidate
	for _, c := range candidates {
		if c.dated {
			timed = append(timed, c)
		}
	}

	var header string
	switch {
	case len(timed) > 0:
		newest := timed[0]
		for _, c := range timed[1:] {
			if c.stamp.After(newest.stamp) {
				newest = c
			}
		}
		var ties []candidate
		seen := map[string]bool{}
		for _, c := range timed {
			if !c.stamp.Equal(newest.stamp) {
				continue
			}
			// A message is identified by who sent it and when, not by its
			// full rendering. OWA nests the same message inside a container
			// that repeats its header and then adds more body, so the two
			// texts differ - deduping on the whole text kept both, every
			// ordinary single-message thread looked like a tie, and auto-send
			// could never fire on a real mailbox at all.
			key := core.NormalizeText(core.SenderPart(c.text))
			if seen[key] {
				continue
			}
			seen[key] = true
			ties = append(ties, c)
		}
		if len(ties) > 1 {
			// Two messages in the same minute. This used to break the tie by
			// screen position - taking the bottom-most, which is the OLDEST
			// under "show newest messages on top", the very setting this file
			// warns about fourteen lines up. A tie is ambiguity, and ambiguity
			// hands the case back rather than guessing.
			// Say WHAT tied. "two messages share a timestamp" is not enough
			// to tell a real ambiguity from the same message counted twice,
			// and without that the only options are to loosen the guard
			// blindly or leave auto-send unable to fire at all.
			log.say("two messages share the newest timestamp (%s) - cannot "+
				"tell which is latest, so auto-send will not apply",
				newest.stamp.Format("15:04"))
			for i, c := range ties {
				log.say("    tied %d: %q", i+1, truncate(
					strings.Join(strings.Fields(c.text), " "), 150))
			}
			return "", body
		}
		header = newest.text
	case len(candidates) == 1:
		header = candidates[0].text
	default:
		// several messages, none with a readable time: cannot tell which is
		// newest, so say so rather than guess
		log.say("could not order %d messages in the thread by time",
			len(candidates))
		return "", body
	}

	return core.SenderPart(header), body
}

var sendSelectors = []string{
	"button[aria-label='Send']",
	"button[aria-label^='Send (']",
	"button[title^='Send']",
}

// ComposeSubject returns the subject line of the open draft, if readable.
func ComposeSubject(p *Page) string {
	el := p.FindFirst(subjectFieldSelectors...)
	if el == nil {
		return ""
	}
	if v, _ := p.fieldValue(el); strings.TrimSpace(v) != "" {
		// covers the contenteditable an inline reply-all uses, where .value
		// is always empty and the old lookup silently found nothing
		return v
	}
	if v := p.AttrOf(el, "value"); v != "" {
		return v
	}
	if v := p.AttrOf(el, "title"); v != "" {
		return v
	}
	return p.TextOf(el)
}

// ClickSend presses Send on the open draft.
//
// With num, the draft's subject must carry that case number first. The Send
// button is found by DOM order, so without this an unrelated draft - one left
// open from an earlier case - could be the one that goes out.
func ClickSend(p *Page, num string, log Logf) bool {
	if num != "" {
		subject := ComposeSubject(p)
		if subject != "" && !core.LabelMatchesCase(subject, num) {
			log.say("refusing to send: draft subject %q is not case %s",
				truncate(subject, 80), num)
			return false
		}
		if subject == "" {
			// An inline reply-all shows no subject field at all, so there is
			// nothing to read and refusing outright would disable auto-send
			// entirely. Fall back to the conversation on screen, which must
			// still be this case - the same check ProcessCase made before it
			// clicked Reply all.
			if pane := ReadingPane(p); pane != nil &&
				core.LabelMatchesCase(p.TextOf(pane), num) {
				log.say("no subject field on this draft (an inline reply); "+
					"the open conversation is still case %s, so sending", num)
				return p.Click(p.FindFirst(sendSelectors...))
			}
			// Nothing can confirm what this draft is. It used to log and send
			// anyway, which made the guard fail OPEN: the one case where the
			// tool cannot see what it is about to send is the case where it
			// must not send.
			log.say("refusing to send: could not read the draft subject, so "+
				"there is no way to confirm this draft is case %s", num)
			return false
		}
	}
	return p.Click(p.FindFirst(sendSelectors...))
}

// ComposeIsOpen reports whether a draft is on screen.
func ComposeIsOpen(p *Page) bool {
	return p.FindFirst(sendSelectors...) != nil
}

// WaitForReadingPane reports true once the clicked mail is open.
//
// When num is given, the pane must also be showing THAT case - the previous
// case's mail is still on screen for a moment after clicking, and acting on it
// would reply to, or auto-send about, the wrong thread.
func WaitForReadingPane(p *Page, timeout time.Duration, num string, log Logf) bool {
	deadline := time.Now().Add(timeout)
	controlsSeen := false
	for time.Now().Before(deadline) {
		hasControls := p.FindFirst(
			"button[aria-label='Reply all']",
			"button[aria-label='Reply']",
			"[aria-label='Forward']",
			"button[aria-label='More options']",
			"button[aria-label='More actions']",
		) != nil || p.FindByText("Forward") != nil

		if hasControls {
			controlsSeen = true
			if num == "" {
				return true
			}
			pane := ReadingPane(p)
			text := ""
			if pane != nil {
				text = p.TextOf(pane)
			}
			if text != "" && core.LabelMatchesCase(text, num) {
				return true
			}
		}
		p.Sleep(500 * time.Millisecond)
	}
	if controlsSeen && num != "" {
		log.say("reading pane never showed case %s - not acting on it", num)
	}
	return false
}

// FindFolder locates a folder in the OWA left nav, or nil.
//
// Outlook prefixes every folder with a private-use icon glyph, so the nav entry
// reads " Sent Items" and neither an exact title match nor exact text
// finds it. That is the trap NormalizeText exists for, and this was the one
// lookup not going through it - which is why Sent Items was unreachable on a
// real mailbox and every send came back "could not check" rather than
// confirmed.
//
// The diagnostic reports through this same function on purpose: a probe that
// looked for folders differently from the code being diagnosed told the reader
// "NOT found" for a folder the tool could reach perfectly well.
func FindFolder(p *Page, name string) *Element {
	if el := p.FindFirst(
		"[role='treeitem'][title='"+name+"']",
		"[title='"+name+"']",
	); el != nil {
		return el
	}
	if el := p.FindByText(name, "treeitem", "button", "link", "menuitem"); el != nil {
		return el
	}
	want := core.NormalizeText(name)
	for _, cand := range p.Find("[role='treeitem']", "[role='tree'] [role='treeitem']") {
		text := cand.Label
		if text == "" {
			text = cand.Text
		}
		if strings.Contains(core.NormalizeText(text), want) {
			found := cand
			return &found
		}
	}
	return nil
}

// ClickFolder clicks a folder in the OWA left nav.
func ClickFolder(p *Page, name string) bool {
	el := FindFolder(p, name)
	if el == nil {
		return false
	}
	// What the list showed before the click, so a click that changed nothing
	// can be told from one that worked.
	before := firstRowLabel(p)

	if !p.Click(el) && !p.ClickJS(el) {
		return false
	}
	p.Sleep(1500 * time.Millisecond)

	// Confirm the view really changed. A swallowed click used to return true
	// with the mailbox still on the inbox, so VerifySent read the case's own
	// search results - rows chosen because they match the case number - and
	// reported them as proof the reply had been sent.
	//
	// Three signals, any one of which is enough. No single one is reliable
	// across OWA builds, and demanding a particular one made verification
	// impossible on a real mailbox: every send came back "could not check",
	// so a confirmed green was unreachable.
	want := core.NormalizeText(name)
	for deadline := time.Now().Add(6 * time.Second); ; {
		if sel := p.FindFirst("[role='treeitem'][aria-selected='true']",
			"[role='treeitem'][title='"+name+"'][aria-selected='true']",
			"[aria-selected='true'][title='"+name+"']"); sel != nil {
			if strings.Contains(core.NormalizeText(p.TextOf(sel)), want) ||
				strings.Contains(core.NormalizeText(p.AttrOf(sel, "title")), want) {
				return true
			}
		}
		if strings.Contains(core.NormalizeText(p.CurrentURL()), want) {
			return true
		}
		if now := firstRowLabel(p); now != "" && now != before {
			return true // the list is showing something else now
		}
		if !time.Now().Before(deadline) {
			return false
		}
		p.Sleep(500 * time.Millisecond)
	}
}

// firstRowLabel is a cheap signature of what the message list is showing.
func firstRowLabel(p *Page) string {
	rows := VisibleResults(p)
	if len(rows) == 0 {
		return ""
	}
	return rows[0].Label
}

// SentResult is what a Sent Items check concluded.
type SentResult int

const (
	// SentUnknown means the check could not run - never treat this as proof
	// either way.
	SentUnknown SentResult = iota
	// SentFound means a matching reply is in Sent Items.
	SentFound
	// SentMissing means the folder was read and the reply was not there.
	SentMissing
)

// VerifySent looks for the reply in Sent Items after the draft closed.
//
// since is when this send happened: a reply older than that belongs to an
// earlier run, and counting it would confirm a send that never left.
func VerifySent(p *Page, num string, since time.Time, log Logf) SentResult {
	// "Sent Items" is the Exchange name. Consumer Outlook calls the same
	// folder "Sent", and a mailbox in another language calls it something
	// else again - so try the names rather than assuming one. Getting this
	// wrong is not harmless: the folder is never reached, every send comes
	// back "could not check", and a confirmed green becomes unreachable.
	opened := false
	for _, name := range []string{"Sent Items", "Sent", "Sent Mail"} {
		if ClickFolder(p, name) {
			log.say("opened the %q folder", name)
			opened = true
			break
		}
	}
	if !opened {
		log.say("could not open a Sent folder - tried Sent Items, Sent, Sent Mail")
		return SentUnknown
	}
	defer ClickFolder(p, "Inbox")

	// A just-sent reply sits in Outbox briefly and the folder list lags behind
	// it. Being impatient here reports a perfectly good send as unverified,
	// which sends the analyst chasing nothing - so give it a proper window and
	// re-open the folder half way through.
	for attempt := 0; attempt < 8; attempt++ {
		// must use the scoped lookup: the raw listbox selector also matches
		// leftover search suggestions, which would "verify" a reply that was
		// never sent
		rows := VisibleResults(p)
		if len(rows) > 12 {
			rows = rows[:12]
		}
		for _, it := range rows {
			if !core.LabelMatchesCase(it.Label, num) {
				continue
			}
			if !since.IsZero() {
				stamp, ok := core.ParseRowTime(it.Label, time.Time{})
				if !ok {
					// An unreadable date is not evidence. Falling through here
					// accepted the row as proof of a fresh send - ambiguity
					// resolved as confirmation, which is the one thing this
					// check must never do.
					log.say("ignoring a reply for %s whose date could not be "+
						"read: %q", num, truncate(it.Label, 80))
					continue
				}
				if stamp.Before(since.Add(-5 * time.Minute)) {
					log.say("ignoring an older reply for %s (%s)", num, stamp)
					continue // a reply from a previous run
				}
			}
			log.say("sent-items hit for %s on attempt %d", num, attempt+1)
			return SentFound
		}
		if attempt == 3 {
			ClickFolder(p, "Inbox") // force the list to refresh
			p.Sleep(600 * time.Millisecond)
			ClickFolder(p, "Sent Items")
		}
		p.Sleep(1500 * time.Millisecond)
	}
	return SentMissing
}

// NewSentWatcher reports true once a compose pane was seen open and has since
// closed (sent or discarded). Two consecutive "closed" polls guard against UI
// re-renders.
func NewSentWatcher(p *Page) func() bool {
	seenOpen := false
	closed := 0
	return func() bool {
		if ComposeIsOpen(p) {
			seenOpen = true
			closed = 0
			return false
		}
		if !seenOpen {
			return false
		}
		closed++
		return closed >= 2
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
