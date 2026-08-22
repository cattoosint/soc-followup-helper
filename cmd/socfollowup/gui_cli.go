//go:build cli

package main

// A console-only build: no tracker window, no self-test.
//
// Why this exists. Getting source onto a locked-down desktop is the hard part
// of this project, and one route - email - runs it through a gateway that
// strips or rewrites attachments containing an HTML document. Two files in
// this repository are exactly that: `internal/web/page.go`, which IS the
// tracker page, and `internal/fakeowa/server.go`, the stand-in Outlook the
// self-test drives. When they do not arrive, nothing builds, because
// `main.go` imports both.
//
// Built with `-tags cli`, main.go imports neither, so the tool builds from
// what is there. This is not a way of sneaking the HTML through - a cli build
// genuinely does not contain it, and the two features that need it genuinely
// are not present.
//
// What is lost:
//
//   - the tracker window. The console does the same run: it prompts in the
//     terminal instead of the browser, and writes the same results CSV and
//     colour-coded review sheet.
//   - `--self-test`, which is the one command that proves the tool works on a
//     machine before it touches real mail. Use `--diagnose SOC<case>` in its
//     place: it needs a real mailbox but opens no mail and sends nothing, and
//     it reports whether the browser, the selectors and the date format work
//     on that build of Outlook.
//
// What is NOT lost: searching, opening the newest matching mail, Reply all,
// the analyst's send, auto-send and its guards, the Sent Items check, the
// results CSV and the review sheet. A full build is preferable; this one is
// complete for the job.

import "fmt"

// windowOptions keeps main.go's call site identical in both builds.
type windowOptions struct {
	URL        string
	ProfileDir string
	OutputDir  string
	AutoSender string
	AutoPhrase string
	ChromePath string
}

func runWindow(windowOptions) int {
	fmt.Println("This is a console-only build - there is no tracker window.")
	fmt.Println()
	fmt.Println("Work through an export from here instead:")
	fmt.Println()
	fmt.Println("    socfollowup.exe --csv \"shift.xlsx\" --check    see what it would do")
	fmt.Println("    socfollowup.exe --csv \"shift.xlsx\"            work through it")
	fmt.Println()
	fmt.Println("The run is the same one the window drives; it asks its")
	fmt.Println("questions here and writes the same two output files.")
	return 2
}

func runSelfTest(bool, string, bool) int {
	fmt.Println("This is a console-only build - --self-test is not included.")
	fmt.Println()
	fmt.Println("It needs the stand-in mailbox (internal/fakeowa), which a full")
	fmt.Println("build has and this one does not.")
	fmt.Println()
	fmt.Println("The nearest check that works here, against a real mailbox:")
	fmt.Println()
	fmt.Println("    socfollowup.exe --diagnose SOC123456")
	fmt.Println()
	fmt.Println("It signs in, searches for that one case, and reports which link")
	fmt.Println("in the chain works or breaks. It opens no mail and sends nothing.")
	return 2
}
