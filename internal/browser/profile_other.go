//go:build !windows

package browser

// releaseStaleProfile is a no-op away from Windows. The tool only ships for
// Windows; this exists so the package still builds elsewhere.
func releaseStaleProfile(profileDir, exeName string) int { return 0 }
