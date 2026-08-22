//go:build !cli

package main

// The tracker window and the self-test. Both are excluded from a build tagged
// "cli", which is how the tool is built on a machine where internal/web and
// internal/fakeowa are not present.
//
// Neither is used when the tool runs against a real mailbox: the tracker is a
// front-end for the same engine the console drives, and fakeowa is a stand-in
// Outlook that exists only for testing. What a "cli" build loses is the window
// and --self-test, not any part of the follow-up itself.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/browser"
	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/engine"
	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
	"github.com/cattoosint/socfollowup-test/internal/web"
)

// windowOptions is what main hands the tracker. It mirrors web.Defaults so
// main.go itself never names the web package.
type windowOptions = web.Defaults

// runWindow serves the tracker and opens it in the analyst's normal browser -
// deliberately not the automated one, which is busy driving Outlook.
func runWindow(d web.Defaults) int {
	srv := web.New(d)
	url, err := srv.Serve(context.Background(), true)
	if err != nil {
		fmt.Println("could not start the tracker:", err)
		return 1
	}
	defer srv.Shutdown()

	fmt.Println("SOC night-shift follow-up")
	fmt.Println()
	fmt.Println("  Tracker:", url)
	fmt.Println()
	fmt.Println("A browser tab should have opened. If it did not, paste that")
	fmt.Println("address into your browser. Leave this window open while a run")
	fmt.Println("is going - closing it stops the tool.")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	fmt.Println()
	fmt.Println("Stopping.")
	return 0
}

// runSelfTest drives the whole tool against a stand-in mailbox served from
// this executable.
//
// It exists for exactly the situation this tool ships into: a locked-down
// desktop where you need to know the thing runs before trusting it with a real
// mailbox. Nothing leaves the machine and no mail can be sent.
func runSelfTest(headless bool, chromePath string, verbose bool) int {
	fmt.Println("Self-test: driving a stand-in mailbox. No real mail is touched.")
	fmt.Println()

	server := fakeowa.Start()
	defer server.Close()

	dir, err := os.MkdirTemp("", "socfu_selftest_")
	if err != nil {
		fmt.Println("could not make a working directory:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	sheet := filepath.Join(dir, "self-test cases.csv")
	if err := writeSelfTestSheet(sheet); err != nil {
		fmt.Println("could not write the test export:", err)
		return 1
	}

	col, cases, err := core.ExtractCases(sheet, "", nil)
	if err != nil {
		fmt.Println("could not read the test export:", err)
		return 1
	}

	logf := browser.Logf(func(string, ...any) {})
	if verbose {
		logf = func(format string, args ...any) {
			fmt.Printf("      [log] "+format+"\n", args...)
		}
	}

	ui := &scriptUI{answer: "s"}
	summary := engine.Run(context.Background(), sheet, col, cases,
		engine.Options{
			URL:         server.URL,
			ProfileDir:  filepath.Join(dir, "profile"),
			OutputDir:   dir,
			Settle:      300 * time.Millisecond,
			AutoSend:    true,
			AutoKeyword: "follow up",
			AutoSender:  "jordan@example.com",
			NoPause:     true,
			Headless:    headless,
			Unattended:  true,
			ChromePath:  chromePath,
		}, ui, logf)

	if summary.RunError != nil {
		fmt.Println("FAILED:", summary.RunError)
		return 1
	}

	// what each case proves
	want := map[string]string{
		"SOC610529": core.StatusSent,     // genuine sender: auto-sent, confirmed
		"SOC700001": core.StatusSkipped,  // quoted sender: refused, handed back
		"SOC999999": core.StatusNotFound, // no mail: flagged, not guessed at
	}
	labels := map[string]string{
		"SOC610529": "auto-sends when the newest mail really is from the sender",
		"SOC700001": "refuses when the sender is only quoted",
		"SOC999999": "flags a case with no matching mail",
	}

	ok := true
	fmt.Println()
	for _, r := range summary.Results {
		pass := r.Status == want[r.Case]
		mark := "ok  "
		if !pass {
			mark, ok = "FAIL", false
		}
		fmt.Printf("[%s] %s - %s\n", mark, labels[r.Case], r.Case)
		if !pass {
			fmt.Printf("       got %s, want %s (%s)\n", r.Status, want[r.Case],
				r.Detail)
		}
	}
	if summary.XLSXPath == "" {
		fmt.Println("[FAIL] no review sheet was written")
		ok = false
	} else {
		fmt.Println("[ok  ] wrote a colour-coded review sheet")
	}

	fmt.Println()
	if !ok {
		fmt.Println("Self-test FAILED - do not use this build against a real mailbox.")
		return 1
	}
	fmt.Println("Self-test passed. Chrome, the browser automation, the matching " +
		"rules,\nthe auto-send guards and the review sheet all work on this machine.")
	return 0
}

func writeSelfTestSheet(path string) error {
	// A CSV keeps the self-test's input independent of the spreadsheet writer
	// it is also checking - the review sheet it produces is still .xlsx.
	body := "Case ID,Summary\n610529,suspicious login\n700001,quoted thread\n" +
		"999999,no mail for this one\n"
	return os.WriteFile(path, []byte(body), 0o600)
}
