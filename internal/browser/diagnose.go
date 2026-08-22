package browser

import (
	"fmt"
	"strings"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/core"
)

// Diagnose answers one question: when a search finds nothing, which link in
// the chain actually broke?
//
// "No mail matched" has several very different causes that look identical from
// the outside - the search box never took the text, Outlook really has no such
// mail, the result rows are there but this OWA build labels them differently,
// or the rows are found but their timestamps are in a format ParseRowTime does
// not read. Guessing between those costs a round trip to whoever has the
// mailbox. This reports which one it is.
//
// It types in the search box and reads the page. It opens nothing, clicks no
// mail, and sends nothing.
func Diagnose(p *Page, num string, settle time.Duration) string {
	var b strings.Builder
	say := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\n", args...)
	}

	say("Search diagnosis for case %s", num)
	say("=====================================")
	say("")
	say("Mailbox URL : %s", p.CurrentURL())
	say("Dates read as: %s", dateOrder())
	say("")

	// The inbox as it stands, before anything is typed. This is the control:
	// if these rows are invisible to every selector, no search was ever going
	// to work, and that is true whether or not this mailbox holds the case.
	say("--- the message list before any search")
	baseline(&b, p)
	say("")

	// The folder list matters as much as the message list: Sent Items is how
	// a send is confirmed, and a folder the tool cannot reach means every
	// send is reported "could not check" and a confirmed green is
	// unreachable. This was found on a live mailbox, so it is reported here
	// rather than guessed at from selectors.
	say("--- the folder list (used to confirm a send)")
	folderReport(&b, p)
	say("")

	box := GetSearchBox(p)
	if box == nil {
		say("FAULT: the search box was not found at all.")
		say("")
		say("Nothing else can work without it. Either the mailbox has not")
		say("finished loading, this is a sign-in page, or this OWA build uses")
		say("markup the search-box selectors do not cover:")
		for _, css := range searchBoxSelectors {
			say("    %s", css)
		}
		return b.String()
	}
	say("Search box  : found")
	say("")

	for i, query := range core.BuildQueries(num) {
		say("--- search %d of 3: %q", i+1, query)

		if err := p.Type(box, query); err != nil {
			say("    FAULT: could not type it: %v", err)
			continue
		}
		p.Sleep(400 * time.Millisecond)
		if err := p.PressEnter(); err != nil {
			say("    FAULT: could not submit it: %v", err)
			continue
		}
		p.Sleep(settle)
		DismissSuggestions(p)

		// Wait the way RunSearch waits. A single snapshot taken ~3.6s after
		// Enter disagreed with the code it is supposed to be diagnosing: on a
		// mailbox rendering results at 5s, RunSearch found the mail while the
		// report said "no rows matched ANY selector - the row markup is what
		// needs looking at", and sent the reader after the wrong subsystem.
		waitForRows(p, 12*time.Second)

		reportOneSearch(&b, p, num)
		say("")
		box = GetSearchBox(p) // the box is re-rendered between searches
		if box == nil {
			say("    the search box disappeared after this search - stopping.")
			break
		}
	}

	say("=====================================")
	say("Send this file back. It says nothing about the contents of any mail")
	say("beyond the row labels Outlook itself shows in the list.")
	return b.String()
}

// baseline reports what the message list looks like with no search running.
//
// It answers the question that matters most and does not depend on this
// mailbox holding any particular mail: can this tool see Outlook's message
// rows at all on this build, and can it read the dates they show?
func baseline(b *strings.Builder, p *Page) {
	say := func(format string, args ...any) {
		fmt.Fprintf(b, format+"\n", args...)
	}

	var winner []Element
	winnerCSS := ""
	for _, css := range MessageListSelectors {
		found := p.Find(css)
		say("    %d rows  %s", len(found), css)
		if len(found) > 0 && winnerCSS == "" {
			winnerCSS, winner = css, found
		}
	}

	if winnerCSS == "" {
		say("    VERDICT: this tool cannot see a single message row in the")
		say("             inbox. Nothing downstream can work on this mailbox,")
		say("             regardless of which case is searched for. The row")
		say("             markup on this OWA build is what needs looking at.")
		return
	}

	dated := 0
	for i, e := range winner {
		_, timeOK := core.ParseRowTime(e.Label, time.Time{})
		if timeOK {
			dated++
		}
		if i < 5 {
			say("      row %d: time=%s  %q", i+1, yesNo(timeOK), trim(e.Label, 110))
		}
	}
	if len(winner) > 5 {
		say("      ... and %d more", len(winner)-5)
	}

	say("    using: %s", winnerCSS)
	say("    %d of %d inbox rows carry a readable date", dated, len(winner))
	switch {
	case dated == len(winner):
		say("    VERDICT: rows are visible and their dates are readable. The")
		say("             selectors and the date format both work here.")
	case dated == 0:
		say("    VERDICT: rows are visible, but NOT ONE date can be read. This")
		say("             breaks newest-first ordering everywhere, and drops")
		say("             rows entirely when only the last-resort selector")
		say("             matches. The date format above needs supporting.")
	default:
		say("    VERDICT: rows are visible, but %d of %d dates cannot be read.",
			len(winner)-dated, len(winner))
		say("             Ordering by newest is unreliable on this mailbox.")
	}
}

// folderReport lists what the left-hand folder nav actually looks like.
func folderReport(b *strings.Builder, p *Page) {
	say := func(format string, args ...any) {
		fmt.Fprintf(b, format+"\n", args...)
	}

	found := false
	for _, css := range []string{
		"[role='treeitem']",
		"[role='tree'] [role='treeitem']",
		"div[role='navigation'] [role='treeitem']",
		"[role='navigation'] a",
		"nav a",
	} {
		items := p.Find(css)
		say("    %d item(s)  %s", len(items), css)
		if len(items) == 0 || found {
			continue
		}
		found = true
		shown := 0
		for _, e := range items {
			label := trim(e.Label, 60)
			if label == "" {
				label = trim(e.Text, 60)
			}
			if label == "" {
				continue
			}
			if shown < 12 {
				say("        %q", label)
				shown++
			}
		}
	}

	// Through the SAME lookup the tool uses, or the report contradicts the
	// code it is meant to be diagnosing.
	for _, name := range []string{"Sent Items", "Sent", "Sent Mail"} {
		say("    %-12s -> %s", name, foundWord(FindFolder(p, name) != nil))
	}
	if !found {
		say("    VERDICT: no folder list is visible. A send cannot be confirmed")
		say("             on this mailbox, so every reply will be reported")
		say("             \"could not check\" rather than confirmed.")
	}
}

func foundWord(v bool) string {
	if v {
		return "found"
	}
	return "NOT found"
}

// waitForRows polls until the message list has rows, matching the patience
// RunSearch has, so the diagnosis describes the same page the tool sees.
func waitForRows(p *Page, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(VisibleResults(p)) > 0 {
			p.Sleep(800 * time.Millisecond) // let the list settle, as RunSearch does
			return
		}
		if core.NoResultsRE.MatchString(p.BodyText()) {
			return
		}
		p.Sleep(600 * time.Millisecond)
	}
}

// reportOneSearch walks every result selector in order and says what each one
// saw, so a build whose markup differs is visible rather than inferred.
func reportOneSearch(b *strings.Builder, p *Page, num string) {
	say := func(format string, args ...any) {
		fmt.Fprintf(b, format+"\n", args...)
	}

	anyRows := false
	var winner []Element
	winnerCSS := ""

	for _, css := range MessageListSelectors {
		found := p.Find(css)
		if len(found) == 0 {
			say("    0 rows  %s", css)
			continue
		}
		anyRows = true
		say("    %d rows  %s", len(found), css)
		if winnerCSS == "" {
			winnerCSS, winner = css, found
		}
	}

	if !anyRows {
		if core.NoResultsRE.MatchString(p.BodyText()) {
			say("    RESULT: Outlook itself says there are no results.")
			say("            The mailbox genuinely has no mail matching this")
			say("            search - not a fault in the tool.")
			return
		}
		say("    RESULT: no rows matched ANY selector, and Outlook did not say")
		say("            'no results' either. That points at this OWA build")
		say("            laying out the message list differently. The row")
		say("            markup is what needs looking at.")
		return
	}

	// Rows exist. Do they carry a readable timestamp, and do any name the case?
	//
	// The timestamp is counted separately for the rows that match, because
	// those are the only ones whose order decides anything. A mailbox where
	// every other row is dated and the matching one is not looks healthy by
	// any total, and is exactly the case that picks the wrong mail to answer.
	dated, matched, matchedDated := 0, 0, 0
	say("    using: %s", winnerCSS)
	for i, e := range winner {
		_, timeOK := core.ParseRowTime(e.Label, time.Time{})
		caseOK := core.LabelMatchesCase(e.Label, num)
		if timeOK {
			dated++
		}
		if caseOK {
			matched++
			if timeOK {
				matchedDated++
			}
		}
		if i < 5 {
			say("      row %d: time=%s case=%s  %q",
				i+1, yesNo(timeOK), yesNo(caseOK), trim(e.Label, 110))
		}
	}
	if len(winner) > 5 {
		say("      ... and %d more", len(winner)-5)
	}

	say("    %d of %d rows carry a timestamp this tool can read", dated, len(winner))
	say("    %d of %d rows name case %s", matched, len(winner), num)

	switch {
	case matched > 0 && matchedDated == matched:
		say("    RESULT: this search works. %d row(s) match.", matched)
	case matchedDated == 0 && winnerCSS == MessageListSelectors[len(MessageListSelectors)-1]:
		// The last-resort selector drops undated rows, so the production
		// search returns NOTHING here. Saying "the search is fine" would be
		// the opposite of what RunSearch does.
		say("    RESULT: %d row(s) match, but not one has a readable date, and", matched)
		say("            on this selector the tool DROPS undated rows - so the")
		say("            real search finds nothing at all here.")
		say("            The date format in the row labels above is what needs")
		say("            supporting.")
	case matched > 0:
		say("    RESULT: %d row(s) match, but %d of them has no readable",
			matched, matched-matchedDated)
		say("            timestamp. The search is fine; the ORDER is not.")
		say("            Newest-first is what decides which mail gets the")
		say("            reply, and it has nothing to sort those rows on. The")
		say("            date format in the row labels above needs supporting")
		say("            before this mailbox can be trusted.")
	case dated == 0:
		say("    RESULT: rows are found, but NONE has a readable timestamp.")
		say("            That matters twice over: the last-resort selector")
		say("            drops undated rows entirely, and ordering by newest")
		say("            depends on reading them. The date format in the row")
		say("            labels above is what needs supporting.")
	default:
		say("    RESULT: rows are found and readable, but none names %s.", num)
		say("            This search returned other mail - most likely the")
		say("            mailbox has no mail for this case, or the search is")
		say("            scoped to a folder that does not hold it.")
	}
}

func dateOrder() string {
	if core.DateIsDayFirst() {
		return "day first (21/08 = 21 August)"
	}
	return "month first (08/21 = 21 August)"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "NO "
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
