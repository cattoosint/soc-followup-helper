//go:build windows

package cdp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindChrome locates a Chromium-based browser to drive.
//
// chromedp did this for us. The order matters: a real Chrome install first,
// then Edge as a last resort. Edge is Chromium and speaks the same protocol,
// which matters on a locked-down desktop where it is the browser that is
// actually present.
func FindChrome() (string, error) {
	found := FindBrowsers()
	if len(found) == 0 {
		return "", errors.New("could not find Chrome or Edge. Install one, or " +
			"pass the full path to chrome.exe with --chrome")
	}
	return found[0].Path, nil
}

// Install is one browser install this machine could be driven with.
type Install struct {
	Name string // "Chrome", "Edge", ...
	Path string
	How  string // where it was found, so a surprising choice can be explained
}

// FindBrowsers lists every browser found, best first.
//
// The usual install folders are checked, but so is the App Paths registry key,
// because a desktop built by IT can put Chrome somewhere else entirely - and a
// picker that only knows the usual folders would silently fall through to Edge
// on a machine that does have Chrome.
func FindBrowsers() []Install {
	var out []Install
	seen := map[string]bool{}
	add := func(name, path, how string) {
		if path == "" {
			return
		}
		key := strings.ToLower(path)
		if seen[key] {
			return
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return
		}
		seen[key] = true
		out = append(out, Install{Name: name, Path: path, How: how})
	}

	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}

	// Preference is by browser, never by how it was found. Edge is registered
	// in App Paths on every Windows machine, so a registry-first sweep would
	// hand Edge the win on a machine that has Chrome sitting in Program Files.
	// Chrome is searched every way first, then Edge.
	families := []struct {
		exe    string
		folder []string
	}{
		{"chrome.exe", []string{
			filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join("Google", "Chrome Beta", "Application", "chrome.exe"),
			filepath.Join("Chromium", "Application", "chrome.exe"),
		}},
		{"msedge.exe", []string{
			filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
		}},
	}

	for _, f := range families {
		name := browserName(f.exe)
		if p := appPath(f.exe); p != "" {
			add(name, p, "registered in App Paths")
		}
		for _, rel := range f.folder {
			for _, root := range roots {
				add(name, filepath.Join(root, rel), "installed under "+root)
			}
		}
		if p, err := exec.LookPath(f.exe); err == nil {
			add(name, p, "on PATH")
		}
	}
	return out
}

func browserName(exe string) string {
	switch strings.ToLower(exe) {
	case "msedge.exe":
		return "Edge"
	case "chrome.exe", "chrome":
		return "Chrome"
	}
	return exe
}

// appPath reads HKLM/HKCU App Paths, which is where an installer records the
// real location of an executable regardless of where it chose to put it.
func appPath(exe string) string {
	for _, hive := range []string{"HKCU", "HKLM"} {
		key := hive + `\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exe
		out, err := exec.Command("reg", "query", key, "/ve").Output()
		if err != nil {
			continue
		}
		// the default value line looks like:  (Default)    REG_SZ    C:\...\chrome.exe
		for _, line := range strings.Split(string(out), "\n") {
			if i := strings.Index(line, "REG_SZ"); i >= 0 {
				p := strings.TrimSpace(line[i+len("REG_SZ"):])
				p = strings.Trim(p, `"`)
				if p != "" {
					return p
				}
			}
		}
	}
	return ""
}
