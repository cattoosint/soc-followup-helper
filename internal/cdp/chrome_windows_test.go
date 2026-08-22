//go:build windows

package cdp

import (
	"os"
	"strings"
	"testing"
)

// Edge is registered in App Paths on every Windows machine. A picker that
// swept the registry before the install folders would therefore hand Edge the
// win on a machine that has Chrome sitting in Program Files - which is exactly
// the wrong answer, and an easy one to get without noticing.
func TestChromeIsPreferredOverEdge(t *testing.T) {
	found := FindBrowsers()
	if len(found) == 0 {
		t.Skip("no browser on this machine")
	}

	firstChrome, firstEdge := -1, -1
	for i, b := range found {
		if b.Name == "Chrome" && firstChrome < 0 {
			firstChrome = i
		}
		if b.Name == "Edge" && firstEdge < 0 {
			firstEdge = i
		}
	}
	if firstChrome < 0 || firstEdge < 0 {
		t.Skipf("need both browsers to compare; found %d install(s)", len(found))
	}
	if firstChrome > firstEdge {
		t.Errorf("Edge (%s, %s) came before Chrome (%s, %s)",
			found[firstEdge].Path, found[firstEdge].How,
			found[firstChrome].Path, found[firstChrome].How)
	}
}

// Every path handed back must actually be there: FindChrome's answer is passed
// straight to exec, and a stale registry entry pointing at an uninstalled
// browser would otherwise fail at launch with a confusing message.
func TestEveryBrowserFoundExists(t *testing.T) {
	for _, b := range FindBrowsers() {
		info, err := os.Stat(b.Path)
		if err != nil {
			t.Errorf("%s (%s): %v", b.Name, b.How, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("%s (%s): %s is a directory", b.Name, b.How, b.Path)
		}
	}
}

// The same install reached two ways - registry and install folder - is one
// browser, not two.
func TestNoDuplicateInstalls(t *testing.T) {
	seen := map[string]string{}
	for _, b := range FindBrowsers() {
		key := strings.ToLower(b.Path)
		if prev, dup := seen[key]; dup {
			t.Errorf("%s listed twice: %q and %q", b.Path, prev, b.How)
		}
		seen[key] = b.How
	}
}

// FindChrome must agree with the list it is built on, or the startup log names
// one browser while another is launched.
func TestFindChromeReturnsTheFirstListedInstall(t *testing.T) {
	found := FindBrowsers()
	path, err := FindChrome()
	if len(found) == 0 {
		if err == nil {
			t.Fatalf("no browsers listed, yet FindChrome returned %q", path)
		}
		return
	}
	if err != nil {
		t.Fatalf("%d browser(s) listed, yet FindChrome failed: %v", len(found), err)
	}
	if path != found[0].Path {
		t.Errorf("FindChrome = %q, but the list starts with %q", path, found[0].Path)
	}
}
