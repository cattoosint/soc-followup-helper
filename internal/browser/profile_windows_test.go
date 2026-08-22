//go:build windows

package browser

import (
	"os"
	"path/filepath"
	"testing"
)

// The analyst's own Chrome - their tabs, their work - must never be touched.
// Only a process whose command line names THIS profile directory may be ended.

func TestEmptyProfileMatchesNothing(t *testing.T) {
	// An empty needle would match every chrome.exe on the machine.
	pids, err := staleProfilePIDs("", "chrome.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pids) != 0 {
		t.Fatalf("an empty profile path matched %d process(es)", len(pids))
	}
	if n := releaseStaleProfile("", "chrome.exe"); n != 0 {
		t.Fatalf("an empty profile path ended %d process(es)", n)
	}
}

// The real proof: on a machine with ordinary Chrome windows open, a profile
// nothing is using must match none of them.
func TestUnusedProfileMatchesNoRunningChrome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uc_profile_not_in_use")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pids, err := staleProfilePIDs(dir, "chrome.exe")
	if err != nil {
		t.Skipf("could not enumerate processes here: %v", err)
	}
	if len(pids) != 0 {
		t.Fatalf("a profile nobody is using matched %d Chrome process(es) - "+
			"this would kill the analyst's own browser", len(pids))
	}
}

func TestPowerShellQuotingIsEscaped(t *testing.T) {
	cases := map[string]string{
		`C:\Users\a\uc_profile`:   `'C:\Users\a\uc_profile'`,
		`C:\Users\o'brien\uc`:     `'C:\Users\o''brien\uc'`,
		`C:\Users\a b\uc_profile`: `'C:\Users\a b\uc_profile'`,
	}
	for in, want := range cases {
		if got := psQuote(in); got != want {
			t.Errorf("psQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// A path holding wildcard characters must not widen the match. This is why the
// query uses Contains rather than -like.
func TestWildcardsInAPathDoNotWidenTheMatch(t *testing.T) {
	// No MkdirAll. "*" is an illegal filename character on Windows, so
	// creating this path always failed and the test always skipped - the only
	// skip in the suite, leaving the property CLAUDE.md calls out with no live
	// coverage at all. staleProfilePIDs never touches the filesystem, so the
	// directory does not need to exist.
	dir := filepath.Join(t.TempDir(), "uc[*]profile")
	pids, err := staleProfilePIDs(dir, "chrome.exe")
	if err != nil {
		t.Skipf("could not enumerate processes here: %v", err)
	}
	if len(pids) != 0 {
		t.Fatalf("a wildcard path matched %d process(es)", len(pids))
	}
}

// A guard hard-coded to chrome.exe silently never fires when the tool is
// driving Edge. Whether that happens because Chrome is absent or because Edge
// was chosen deliberately does not matter - the guard must follow whatever was
// actually launched.
func TestGuardHuntsForTheBrowserItIsGiven(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uc_profile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, exe := range []string{"chrome.exe", "msedge.exe"} {
		pids, err := staleProfilePIDs(dir, exe)
		if err != nil {
			t.Skipf("could not enumerate processes here: %v", err)
		}
		if len(pids) != 0 {
			t.Errorf("%s: a profile nobody is using matched %d process(es)",
				exe, len(pids))
		}
	}
	// an empty browser name must match nothing, not everything
	if pids, err := staleProfilePIDs(dir, ""); err == nil && len(pids) != 0 {
		t.Errorf("an empty browser name matched %d process(es)", len(pids))
	}
}
