//go:build windows

package core

import (
	"os/exec"
	"strings"
)

// detectDayFirst reads the Windows short-date format, which is what Outlook
// itself follows when it renders "10/8/2026".
//
// This asks PowerShell rather than linking golang.org/x/sys for its registry
// package. That dependency cost 1.6 MB of vendored source for a single string
// lookup, on a tool whose deliverable has to fit through a mail size limit.
// PowerShell is already used elsewhere here - the file picker and the
// stale-profile guard - so it is not a new assumption about the machine.
func detectDayFirst() bool {
	const script = `(Get-ItemProperty -Path 'HKCU:\Control Panel\International' ` +
		`-Name sShortDate -ErrorAction SilentlyContinue).sShortDate`

	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", script).Output()
	if err != nil {
		// Not knowing is not the same as knowing it is month-first, but the
		// caller needs an answer, and month-first is the Windows default.
		return false
	}
	format := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.HasPrefix(format, "d")
}
