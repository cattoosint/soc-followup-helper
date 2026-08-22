//go:build windows

package web

import (
	"os/exec"
	"strings"
)

// chooseFile opens the ordinary Windows "Open file" dialog and returns what
// the analyst picked, or "" if they cancelled.
//
// A browser will not hand a page the real path of a chosen file, and the
// engine needs a real path - so the picker is opened host-side instead. The
// dialog is given a topmost owner because it would otherwise open behind the
// browser window, which looks like nothing happened.
func chooseFile() (string, error) {
	const script = `
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form -Property @{ TopMost = $true }
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = 'Choose the export to work through'
# .xlsx first and selected by default: an Excel filter only survives in a
# workbook, so a CSV would hand back the rows the analyst filtered out.
$dialog.Filter = 'Excel workbook - keeps your filter (*.xlsx;*.xlsm)|*.xlsx;*.xlsm|CSV - filter is lost (*.csv)|*.csv|All files (*.*)|*.*'
$dialog.FilterIndex = 1
$dialog.Multiselect = $false
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::Out.Write($dialog.FileName)
}
$owner.Dispose()
`
	// -STA is required: the file dialog is a COM control and will not open on
	// a multi-threaded apartment.
	out, err := exec.Command("powershell", "-STA", "-NoProfile",
		"-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
