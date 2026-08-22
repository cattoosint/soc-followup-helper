//go:build !windows

package core

// detectDayFirst has no registry to read away from Windows. The tool only
// ships for Windows; this exists so the logic stays testable elsewhere.
func detectDayFirst() bool { return false }
