// Package browser drives Outlook on the web through the Chrome DevTools
// Protocol, using the small client in internal/cdp.
//
// Selenium handed out element handles that could be re-read and clicked. There
// is no such handle here, so the equivalent is a "ref": a data-socfu-ref
// attribute stamped onto the element in the page, which the tool can then
// address by selector. That keeps the logic above this file looking much like
// the reviewed Python original.
//
// Two rules make refs behave like handles rather than like a leak:
//
//   - a ref is stamped once and reused. Re-stamping on every query silently
//     invalidated handles the caller was still holding, and a click on one
//     then waited for an element that no longer existed - forever.
//   - every call into Chrome carries a deadline, so a selector that never
//     matches fails the step instead of wedging the run.
package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/cdp"
)

// Timeouts for one interaction with the page. These bound a single DOM call,
// not the waiting the logic above does - that has its own deadlines.
const (
	queryTimeout  = 15 * time.Second
	clickTimeout  = 15 * time.Second
	launchTimeout = 90 * time.Second
)

// Element is one visible element found in the page.
type Element struct {
	Ref      string  `json:"ref"`
	Label    string  `json:"label"`
	Text     string  `json:"text"`
	Y        float64 `json:"y"`
	Selector string  `json:"selector"`
}

// Handle addresses the element again, by the ref stamped on it.
func (e Element) Handle() string {
	return fmt.Sprintf("[data-socfu-ref=%q]", e.Ref)
}

// Page is one Chrome tab under automation.
type Page struct {
	br     *cdp.Browser
	ctx    context.Context
	cancel context.CancelFunc
}

// Options configures the browser launch.
type Options struct {
	ProfileDir string
	URL        string
	Headless   bool
	// ChromePath is optional; standard locations are searched when empty.
	ChromePath string
	// Log, when set, receives notes about the launch itself.
	Log Logf
}

// Launch starts Chrome against a persistent profile directory, so a signed-in
// session survives between runs.
//
// Chrome cannot automate a profile another Chrome instance holds, and 136+
// refuses automation on the default profile directory - hence a dedicated one.
func Launch(parent context.Context, opts Options) (*Page, error) {
	// Resolve the browser first, so the analyst is told which one they got and
	// so the stale-profile guard hunts for the right executable. On a machine
	// with no Chrome this will be Edge, which drives fine but is worth saying
	// out loud rather than leaving as a surprise.
	if opts.ChromePath == "" {
		found, err := cdp.FindChrome()
		if err != nil {
			return nil, err
		}
		opts.ChromePath = found
	}
	browserName := filepath.Base(opts.ChromePath)
	opts.Log.say("driving %s", opts.ChromePath)
	if !strings.EqualFold(browserName, "chrome.exe") {
		opts.Log.say("note: this is not Chrome. It should still work - %s is "+
			"Chromium-based - but Chrome is what this was proven against.",
			browserName)
	}

	if opts.ProfileDir != "" {
		if err := os.MkdirAll(opts.ProfileDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating profile dir: %w", err)
		}
		abs, err := filepath.Abs(opts.ProfileDir)
		if err != nil {
			return nil, err
		}
		opts.ProfileDir = abs

		// A previous run that died without closing Chrome leaves the profile
		// locked, and the launch below then fails in a way that looks like
		// being signed out. Only processes naming THIS profile are ended -
		// never the analyst's own browser.
		if n := releaseStaleProfile(abs, browserName); n > 0 {
			opts.Log.say("freed the profile from %d leftover %s process(es)",
				n, browserName)
		}
	}

	ctx, cancel := context.WithCancel(parent)

	launchCtx, launchCancel := context.WithTimeout(ctx, launchTimeout)
	defer launchCancel()

	br, err := cdp.Launch(launchCtx, cdp.Options{
		ChromePath: opts.ChromePath,
		ProfileDir: opts.ProfileDir,
		Headless:   opts.Headless,
	})
	if err != nil {
		cancel()
		return nil, err
	}

	p := &Page{br: br, ctx: ctx, cancel: cancel}
	if opts.URL != "" {
		if err := p.Navigate(opts.URL); err != nil {
			p.Close()
			return nil, err
		}
	}
	return p, nil
}

// Close shuts the browser down.
func (p *Page) Close() {
	if p.br != nil {
		p.br.Close()
	}
	if p.cancel != nil {
		p.cancel()
	}
}

// Leave ends the session but leaves the browser window on screen.
func (p *Page) Leave() {
	if p.br != nil {
		p.br.Detach()
	}
	if p.cancel != nil {
		p.cancel()
	}
}

// Context exposes the page's context.
func (p *Page) Context() context.Context { return p.ctx }

// call runs one operation under a deadline, so no single step wedges the run.
func (p *Page) call(timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	return fn(ctx)
}

// eval runs JavaScript and decodes the result.
func (p *Page) eval(timeout time.Duration, js string, out any) error {
	return p.call(timeout, func(ctx context.Context) error {
		return p.br.Eval(ctx, js, out)
	})
}

// Navigate opens a URL.
func (p *Page) Navigate(url string) error {
	return p.call(launchTimeout, func(ctx context.Context) error {
		return p.br.Navigate(ctx, url)
	})
}

// CurrentURL reports where the tab is.
func (p *Page) CurrentURL() string {
	var url string
	err := p.call(queryTimeout, func(ctx context.Context) error {
		var innerErr error
		url, innerErr = p.br.CurrentURL(ctx)
		return innerErr
	})
	if err != nil {
		return ""
	}
	return url
}

// refJS hands out a stable ref: an element already carrying one keeps it, so
// a handle the caller is still holding stays valid across later queries.
const refJS = `
  function socfuRef(el) {
    var ref = el.getAttribute('data-socfu-ref');
    if (!ref) {
      window.__socfu_n = (window.__socfu_n || 0) + 1;
      ref = 'r' + window.__socfu_n;
      el.setAttribute('data-socfu-ref', ref);
    }
    return ref;
  }
  function socfuShown(el) {
    var style = window.getComputedStyle(el);
    return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length) &&
           style.visibility !== 'hidden' && style.display !== 'none';
  }
  function socfuDescribe(el, sel) {
    var rect = el.getBoundingClientRect();
    var label = el.getAttribute('aria-label') || el.innerText || '';
    var text = el.innerText || '';
    return {
      ref: socfuRef(el),
      label: label.replace(/\s+/g, ' ').trim(),
      text: text.replace(/\s+/g, ' ').trim(),
      y: rect.top,
      selector: sel
    };
  }`

// findJS returns the visible matches for the first selector that matches
// anything, mirroring the original _find_first / _visible_results behaviour.
const findJS = refJS + `
(function (selectors) {
  for (var s = 0; s < selectors.length; s++) {
    var els;
    try { els = document.querySelectorAll(selectors[s]); } catch (e) { continue; }
    var found = [];
    for (var i = 0; i < els.length; i++) {
      if (socfuShown(els[i])) { found.push(socfuDescribe(els[i], selectors[s])); }
    }
    if (found.length) { return found; }
  }
  return [];
})(%s)`

// Find returns the visible elements matched by the first selector in the list
// that matches anything.
func (p *Page) Find(selectors ...string) []Element {
	var out []Element
	if err := p.eval(queryTimeout,
		fmt.Sprintf(findJS, jsStringArray(selectors)), &out); err != nil {
		return nil
	}
	return out
}

// FindFirst returns the first visible element matching any selector, or nil.
func (p *Page) FindFirst(selectors ...string) *Element {
	found := p.Find(selectors...)
	if len(found) == 0 {
		return nil
	}
	return &found[0]
}

// findByTextJS locates menu entries that carry no aria-label, by exact text.
const findByTextJS = refJS + `
(function (roles, want) {
  var out = [];
  var nodes = document.querySelectorAll('[role],button');
  for (var i = 0; i < nodes.length; i++) {
    var el = nodes[i];
    var role = el.getAttribute('role') ||
               (el.tagName === 'BUTTON' ? 'button' : '');
    if (roles.indexOf(role) < 0) { continue; }
    var text = (el.innerText || '').replace(/\s+/g, ' ').trim();
    if (text !== want) { continue; }
    if (!socfuShown(el)) { continue; }
    out.push(socfuDescribe(el, 'byText'));
  }
  return out;
})(%s, %s)`

// FindByText returns the first visible element with one of these ARIA roles
// whose text is exactly want.
func (p *Page) FindByText(want string, roles ...string) *Element {
	if len(roles) == 0 {
		roles = []string{"menuitem", "button", "menuitemcheckbox"}
	}
	var out []Element
	js := fmt.Sprintf(findByTextJS, jsStringArray(roles), jsString(want))
	if err := p.eval(queryTimeout, js, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return &out[0]
}

// findWithinJS collects visible matches for every selector inside a root
// element, rather than stopping at the first selector that matches.
//
// ReadLastMessage needs this: it gathers message headers from two different
// selectors at once, and it must look only inside the reading pane - matching
// document-wide would pull in the message list behind it.
const findWithinJS = refJS + `
(function (rootSel, selectors) {
  var root = document.querySelector(rootSel);
  if (!root) { return []; }
  var out = [];
  for (var s = 0; s < selectors.length; s++) {
    var els;
    try { els = root.querySelectorAll(selectors[s]); } catch (e) { continue; }
    for (var i = 0; i < els.length; i++) {
      if (socfuShown(els[i])) { out.push(socfuDescribe(els[i], selectors[s])); }
    }
  }
  return out;
})(%s, %s)`

// FindAllWithin returns every visible match for all selectors inside root.
func (p *Page) FindAllWithin(root *Element, selectors ...string) []Element {
	if root == nil {
		return nil
	}
	var out []Element
	js := fmt.Sprintf(findWithinJS, jsString(root.Handle()),
		jsStringArray(selectors))
	if err := p.eval(queryTimeout, js, &out); err != nil {
		return nil
	}
	return out
}

// rectJS scrolls an element into view and reports where it landed. Clicks are
// dispatched at viewport coordinates, so it has to be on screen first.
const rectJS = `
(function () {
  var e = document.querySelector(%s);
  if (!e) { return {ok: false}; }
  try { e.scrollIntoView({block: 'center', inline: 'center'}); } catch (err) {
    try { e.scrollIntoView(); } catch (err2) {}
  }
  var r = e.getBoundingClientRect();
  return {ok: r.width > 0 && r.height > 0,
          x: r.left, y: r.top, w: r.width, h: r.height};
})()`

type elementRect struct {
	OK bool    `json:"ok"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
}

// Click presses an element found earlier. It reports false rather than
// erroring, so callers can fall back to another route as the original did.
//
// A real mouse event first, because OWA listens for them; the DOM route is
// the fallback when the element cannot be placed on screen.
func (p *Page) Click(el *Element) bool {
	if el == nil {
		return false
	}
	var r elementRect
	if err := p.eval(queryTimeout,
		fmt.Sprintf(rectJS, jsString(el.Handle())), &r); err == nil && r.OK {
		// Only click the point if the element is really what is on top of it.
		// Input.dispatchMouseEvent reports success for coordinates covered by
		// an overlay, or scrolled off screen - so a click that reached nothing
		// came back true, and because it came back true the DOM fallback below
		// could never run. A swallowed click on "Sent Items" left VerifySent
		// reading the inbox and calling it proof.
		if p.hitTestReaches(el, r.X+r.W/2, r.Y+r.H/2) {
			err := p.call(clickTimeout, func(ctx context.Context) error {
				return p.br.ClickAt(ctx, r.X+r.W/2, r.Y+r.H/2)
			})
			if err == nil {
				return true
			}
		}
	}
	// OWA can move a node between the hit test and the click; the DOM route
	// does not depend on coordinates.
	return p.ClickJS(el)
}

// hitTestReaches reports whether the element at (x, y) is el or inside it.
func (p *Page) hitTestReaches(el *Element, x, y float64) bool {
	js := fmt.Sprintf(`(function(){
  var e = document.querySelector(%s);
  if (!e) { return false; }
  var hit = document.elementFromPoint(%f, %f);
  if (!hit) { return false; }
  return e === hit || e.contains(hit) || hit.contains(e);
})()`, jsString(el.Handle()), x, y)
	var ok bool
	if err := p.eval(queryTimeout, js, &ok); err != nil {
		return false
	}
	return ok
}

// ClickJS presses an element through the DOM.
func (p *Page) ClickJS(el *Element) bool {
	if el == nil {
		return false
	}
	js := fmt.Sprintf(
		`(function(){var e=document.querySelector(%s);`+
			`if(!e){return false;} e.click(); return true;})()`,
		jsString(el.Handle()))
	var ok bool
	if err := p.eval(queryTimeout, js, &ok); err != nil {
		return false
	}
	return ok
}

// TextOf re-reads an element's text, which may have changed since it was found.
func (p *Page) TextOf(el *Element) string {
	if el == nil {
		return ""
	}
	js := fmt.Sprintf(
		`(function(){var e=document.querySelector(%s);`+
			`if(!e){return "";}`+
			`return (e.innerText||"").replace(/\s+/g," ").trim();})()`,
		jsString(el.Handle()))
	var out string
	if err := p.eval(queryTimeout, js, &out); err != nil {
		return ""
	}
	return out
}

// AttrOf reads an attribute or, for inputs, the live value.
func (p *Page) AttrOf(el *Element, name string) string {
	if el == nil {
		return ""
	}
	js := fmt.Sprintf(
		`(function(){var e=document.querySelector(%s);`+
			`if(!e){return "";}`+
			`if(%s==="value"&&"value" in e){return e.value||"";}`+
			`return e.getAttribute(%s)||"";})()`,
		jsString(el.Handle()), jsString(name), jsString(name))
	var out string
	if err := p.eval(queryTimeout, js, &out); err != nil {
		return ""
	}
	return out
}

// BodyText returns the visible text of the page.
func (p *Page) BodyText() string {
	var out string
	if err := p.eval(queryTimeout,
		`(document.body && document.body.innerText) || ""`, &out); err != nil {
		return ""
	}
	return out
}

// Type focuses a field, clears it, and types text.
//
// Clearing is load-bearing, not tidiness. If the previous case's query is left
// in the box, the new one is appended and Outlook searches something like
// "SOC610529SOC700001" - which matches the OLD case, so the tool would open,
// and possibly reply to, the wrong mail. The value is therefore read back and
// the typing is refused if it did not land.
func (p *Page) Type(el *Element, text string) error {
	if el == nil {
		return fmt.Errorf("no element to type into")
	}
	if !p.Click(el) {
		return fmt.Errorf("could not focus the field before typing")
	}
	p.Sleep(150 * time.Millisecond)

	if !p.clearField(el) {
		return fmt.Errorf("could not empty the search box before typing")
	}
	if err := p.call(queryTimeout, func(ctx context.Context) error {
		return p.br.TypeText(ctx, text)
	}); err != nil {
		return err
	}
	// Read it back. Not for reassurance: a field that silently kept its old
	// contents is how the previous case's number ended up being searched for.
	//
	// The read has to cover both kinds of field. An <input> holds its text in
	// .value; a contenteditable div - which is what OWA uses for To, Cc and
	// the message body - always reports .value as empty, so checking that
	// alone would fail on every compose field of a real mailbox.
	got, rich := p.fieldValue(el)
	if !fieldHolds(got, text, rich) {
		return fmt.Errorf("the field holds %q, not %q - refusing to carry on "+
			"with the wrong text in it", got, text)
	}
	return nil
}

// fieldValueJS reads whichever of the two a field actually uses, and says
// which kind it was - the answer changes how strictly it can be checked.
const fieldValueJS = `
(function () {
  var e = document.querySelector(%s);
  if (!e) { return { text: '', rich: false }; }
  if (e.isContentEditable) {
    return { text: e.innerText || e.textContent || '', rich: true };
  }
  return { text: e.value || '', rich: false };
})()`

func (p *Page) fieldValue(el *Element) (string, bool) {
	var out struct {
		Text string `json:"text"`
		Rich bool   `json:"rich"`
	}
	js := fmt.Sprintf(fieldValueJS, jsString(el.Handle()))
	if err := p.eval(queryTimeout, js, &out); err != nil {
		return "", false
	}
	return out.Text, out.Rich
}

// fieldHolds reports whether a field ended up holding the text that was typed.
//
// A plain <input> is checked exactly, and that strictness is the point: the
// search box once kept its previous contents and searched "SOC610529SOC700001"
// - which matched the OLD case and replied on the wrong thread. Anything
// looser than equality lets that back in, because the stale text CONTAINS the
// new text.
//
// A contenteditable cannot be held to that, and cannot honestly be held to
// containment either: once OWA commits an address it becomes a chip showing a
// display name, which may share nothing at all with what was typed
// ("Sam Okonkwo" for s.okonkwo@example.com). All that can truthfully be
// asserted about a rich field is that it took something.
//
// That is a weaker check, and it is weaker on purpose rather than by accident.
// What stops a wrong compose being sent is not this - it is ClickSend, which
// re-reads the draft's subject and refuses if it is not the expected case.
func fieldHolds(got, want string, rich bool) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == want {
		return true
	}
	if !rich {
		return false
	}
	return got != ""
}

// clearFieldJS empties a field and tells the page it changed.
//
// Setting the value attribute is not enough: a field the user has typed into
// keeps its live property, and the next keystrokes are appended to it. The
// property has to be set through the prototype's native setter, and an input
// event dispatched, or a framework-driven box like Outlook's keeps rendering
// the stale value.
const clearFieldJS = `
(function () {
  var e = document.querySelector(%s);
  if (!e) { return false; }
  e.focus();
  if (e.isContentEditable) {
    e.textContent = '';
  } else {
    var proto = (typeof HTMLTextAreaElement !== 'undefined' &&
                 e instanceof HTMLTextAreaElement)
      ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    var desc = Object.getOwnPropertyDescriptor(proto, 'value');
    if (desc && desc.set) { desc.set.call(e, ''); } else { e.value = ''; }
  }
  e.dispatchEvent(new Event('input', { bubbles: true }));
  e.dispatchEvent(new Event('change', { bubbles: true }));
  if (e.setSelectionRange) {
    try { e.setSelectionRange(0, 0); } catch (err) {}
  }
  return e.isContentEditable ? e.textContent === '' : e.value === '';
})()`

// clearField empties a field, reporting whether it actually came out empty.
func (p *Page) clearField(el *Element) bool {
	var ok bool
	js := fmt.Sprintf(clearFieldJS, jsString(el.Handle()))
	if err := p.eval(queryTimeout, js, &ok); err != nil {
		return false
	}
	return ok
}

// PressEnter submits the focused field.
func (p *Page) PressEnter() error {
	return p.call(queryTimeout, func(ctx context.Context) error {
		return p.br.PressKey(ctx, cdp.KeyEnter)
	})
}

// PressEscape closes a menu or popup, so nothing is left hanging open for the
// next case.
func (p *Page) PressEscape() error {
	return p.call(queryTimeout, func(ctx context.Context) error {
		return p.br.PressKey(ctx, cdp.KeyEscape)
	})
}

// Screenshot saves the visible page, for a failure the analyst has to look at.
func (p *Page) Screenshot(dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var png []byte
	err := p.call(queryTimeout, func(ctx context.Context) error {
		var innerErr error
		png, innerErr = p.br.Screenshot(ctx)
		return innerErr
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.png", name,
		time.Now().Format("150405")))
	if err := os.WriteFile(path, png, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Sleep pauses, but gives up early if the run is being stopped.
func (p *Page) Sleep(d time.Duration) {
	select {
	case <-time.After(d):
	case <-p.ctx.Done():
	}
}

// jsString renders a Go string as a JavaScript literal.
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsStringArray renders a Go slice as a JavaScript array literal.
func jsStringArray(items []string) string {
	parts := make([]string, 0, len(items))
	for _, s := range items {
		parts = append(parts, jsString(s))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
