//go:build !windows

package web

import "errors"

// chooseFile has no native dialog away from Windows. The tool only ships for
// Windows; the page falls back to drag-and-drop, which works everywhere.
func chooseFile() (string, error) {
	return "", errors.New("no file dialog on this platform - drag the file onto the page instead")
}
