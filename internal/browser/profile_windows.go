//go:build windows

package browser

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Chrome cannot automate a profile another Chrome instance is holding. A run
// that dies without closing the browser leaves one behind, and the next launch
// then fails in a way that looks like being signed out - which cost real time
// to diagnose on the Python build.
//
// The rule that matters: kill ONLY processes whose command line names this
// exact profile directory. The analyst's ordinary Chrome, with their tabs and
// their work in it, must never be touched. Do not loosen this to "chrome.exe".

// staleProfilePIDs lists the processes of browser exeName holding profileDir.
//
// exeName matters: the tool can be driving msedge.exe - by choice or because
// Chrome was not found - and a guard hard-coded to chrome.exe would then
// silently never free anything.
//
// The match is Contains on the full profile path rather than a wildcard, so a
// path containing [ ] * or ? cannot widen it into matching other processes.
func staleProfilePIDs(profileDir, exeName string) ([]int, error) {
	if strings.TrimSpace(profileDir) == "" || strings.TrimSpace(exeName) == "" {
		return nil, nil // never enumerate with an empty needle
	}
	script := fmt.Sprintf(
		"Get-CimInstance Win32_Process -Filter %s | "+
			"Where-Object { $_.CommandLine -and $_.CommandLine.Contains(%s) } | "+
			"Select-Object -ExpandProperty ProcessId",
		psQuote("Name='"+exeName+"'"), psQuote(profileDir))

	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", script).Output()
	if err != nil {
		// Not being able to look is not a reason to start killing things.
		return nil, err
	}

	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		n, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr == nil && n > 0 {
			pids = append(pids, n)
		}
	}
	return pids, nil
}

// psQuote renders a string as a PowerShell single-quoted literal, where the
// only escape is a doubled quote.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// releaseStaleProfile frees profileDir if a previous run left Chrome holding
// it. It reports how many processes it ended.
func releaseStaleProfile(profileDir, exeName string) int {
	pids, err := staleProfilePIDs(profileDir, exeName)
	if err != nil || len(pids) == 0 {
		return 0
	}
	killed := 0
	for _, pid := range pids {
		// /F because a wedged Chrome will not close politely; no /T, so only
		// the matched process goes, not an unrelated tree.
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").
			Run(); err == nil {
			killed++
		}
	}
	return killed
}
