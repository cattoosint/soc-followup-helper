//go:build !windows

package cdp

import (
	"errors"
	"os"
	"os/exec"
)

// FindChrome locates a Chromium-based browser. The tool ships for Windows;
// this exists so the package builds and can be exercised elsewhere.
func FindChrome() (string, error) {
	for _, name := range []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"chrome", "microsoft-edge",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("could not find a Chromium-based browser")
}
