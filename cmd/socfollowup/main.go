// Command socfollowup is the SOC night-shift follow-up helper.
//
// It reads a filtered XSOAR export and, for each case number, searches Outlook
// on the web, opens the newest matching mail, clicks Reply all, and waits for
// the analyst to send. Optionally it can send by itself under a strict rule.
//
// The whole tool is one executable: no interpreter, no driver, no packages to
// install. Chrome is the only thing that has to be on the machine already.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/browser"
	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/engine"
)

// settings holds local preferences. The auto-send sender is site-specific and
// is deliberately never committed - it lives beside the executable.
type settings struct {
	URL        string `json:"url,omitempty"`
	AutoSender string `json:"auto_sender,omitempty"`
	AutoPhrase string `json:"auto_phrase,omitempty"`
}

func settingsPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "soc_settings.json"
	}
	return filepath.Join(filepath.Dir(exe), "soc_settings.json")
}

func loadSettings() settings {
	var s settings
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

func main() {
	saved := loadSettings()

	var (
		sheet    = flag.String("csv", "", "XSOAR export to work through (.xlsx or .csv)")
		column   = flag.String("column", "", "column holding case numbers (auto-detected if omitted)")
		url      = flag.String("url", firstNonEmpty(saved.URL, "https://outlook.office.com/mail/"), "mailbox URL")
		profile  = flag.String("profile-dir", "", "Chrome profile directory (defaults to uc_profile beside the exe)")
		outDir   = flag.String("out-dir", "", "where to write results (defaults to the exe's directory)")
		settle   = flag.Float64("settle", 3.0, "seconds to let search results settle")
		delay    = flag.Float64("send-delay", 5.0, "seconds to wait after a send before the next case")
		autoSend = flag.Bool("auto-send", false, "send automatically when the strict rule matches")
		sender   = flag.String("auto-sender", saved.AutoSender, "auto-send only if the newest mail is from this sender")
		phrase   = flag.String("auto-phrase", firstNonEmpty(saved.AutoPhrase, "follow up"), "auto-send only if the mail contains this phrase")
		noPause  = flag.Bool("no-pause", false, "do not stop on a case with no matching mail")
		limit    = flag.Int("limit", 0, "process at most this many cases (0 = all)")
		check    = flag.Bool("check", false, "read the export and report what a run would do, then exit")
		selfTest = flag.Bool("self-test", false, "prove the tool works on this machine against a stand-in mailbox")
		headless = flag.Bool("headless", false, "run Chrome without a visible window")
		chrome   = flag.String("chrome", "", "path to chrome.exe (found automatically if omitted)")
		verbose  = flag.Bool("verbose", false, "print the browser-level log too")
		window   = flag.Bool("ui", false, "open the tracker window (a local page in your browser)")
		diagnose = flag.String("diagnose", "", "search a real mailbox for one case and report why it did or did not match; opens no mail and sends nothing")
		testMail = flag.String("send-test-mail", "", "send one test mail to this address so an end-to-end check has something to find")
		testCc   = flag.String("test-cc", "", "cc the test mail here, so Reply all has a second recipient")
		testCase = flag.String("test-case", "", "case number for the test mail's subject (default: a fresh one)")
		assumeY  = flag.Bool("yes", false, "skip the confirmation before sending a test mail")
	)
	flag.Parse()

	if *profile == "" {
		*profile = beside("uc_profile")
	}
	if *outDir == "" {
		*outDir = beside("")
	}

	switch {
	case *selfTest:
		os.Exit(runSelfTest(*headless, *chrome, *verbose))
	case *check:
		os.Exit(runCheck(*sheet, *column))
	case *diagnose != "":
		os.Exit(runDiagnose(*diagnose, *url, *profile, *outDir, *chrome, *verbose))
	case *testMail != "":
		os.Exit(runSendTestMail(*testMail, *testCc, *testCase, *url, *profile,
			*chrome, *assumeY, *verbose))
	case *window || (*sheet == "" && flag.NFlag() == 0):
		// Double-clicking the executable lands here, which is how an analyst
		// will actually start it.
		os.Exit(runWindow(windowOptions{
			URL:        *url,
			ProfileDir: *profile,
			OutputDir:  *outDir,
			AutoSender: *sender,
			AutoPhrase: *phrase,
			ChromePath: *chrome,
		}))
	case *sheet == "":
		fmt.Println("SOC night-shift follow-up")
		fmt.Println()
		fmt.Println("  socfollowup                             open the tracker window")
		fmt.Println("  socfollowup --csv <export.xlsx>         work through an export")
		fmt.Println("  socfollowup --csv <export.xlsx> --check see what it would do")
		fmt.Println("  socfollowup --self-test                 prove it runs on this machine")
		fmt.Println()
		flag.PrintDefaults()
		os.Exit(2)
	}

	if *autoSend && strings.TrimSpace(*sender) == "" {
		fmt.Println("--auto-send needs --auto-sender: the address or display " +
			"name the newest mail must be from.")
		os.Exit(2)
	}

	col, cases, err := core.ExtractCases(*sheet, *column, nil)
	if err != nil {
		fmt.Println("Could not read the export:", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}
	fmt.Printf("%d case(s) from column %q\n\n", len(cases), col)

	if *autoSend {
		fmt.Println("!! AUTO-SEND IS ON.")
		fmt.Printf("   A reply is sent with no review when the NEWEST mail in "+
			"the thread is from %q and contains %q.\n", *sender, *phrase)
		fmt.Println("   Check your Outlook signature is right before continuing.")
		if !confirm("   Type YES to continue: ", "YES") {
			fmt.Println("Stopped.")
			os.Exit(0)
		}
		fmt.Println()
	}

	ui := &consoleUI{in: bufio.NewReader(os.Stdin)}
	logf := browser.Logf(func(string, ...any) {})
	if *verbose {
		logf = func(format string, args ...any) {
			fmt.Printf("      [log] "+format+"\n", args...)
		}
	}

	summary := engine.Run(context.Background(), *sheet, col, cases,
		engine.Options{
			URL:         *url,
			ProfileDir:  *profile,
			OutputDir:   *outDir,
			Settle:      time.Duration(*settle * float64(time.Second)),
			SendDelay:   time.Duration(*delay * float64(time.Second)),
			AutoSend:    *autoSend,
			AutoKeyword: *phrase,
			AutoSender:  *sender,
			NoPause:     *noPause,
			Headless:    *headless,
			ChromePath:  *chrome,
		}, ui, logf)

	if summary.CSVPath != "" {
		fmt.Println("Results:", filepath.Base(summary.CSVPath))
	}
	if summary.RunError != nil {
		os.Exit(1)
	}
}

// runSendTestMail composes and sends one test mail, so an end-to-end check has
// something real to find without waiting for a genuine alert to arrive.
//
// This is the only path in the tool that puts a NEW mail into the world. It
// prints the recipients and waits for a typed yes first: a mistyped address
// here does not fail, it delivers to a stranger.
func runSendTestMail(to, cc, caseNum, url, profileDir, chromePath string,
	assumeYes, verbose bool) int {

	to = strings.TrimSpace(to)
	if to == "" {
		fmt.Println("--send-test-mail needs an address, e.g.")
		fmt.Println("    socfollowup.exe --send-test-mail me@example.com")
		return 2
	}
	if caseNum == "" {
		// Six digits, not ten: CaseNumRE is \d{5,8}, so a longer number cannot
		// round-trip through the tool's own extractor - "0821212529" came back
		// as "08212125" and the test mail then read as "no matching mail".
		caseNum = time.Now().Format("150405")
	}

	mail := browser.DefaultTestMail(to, cc, caseNum)

	fmt.Println("About to send a real mail from your mailbox:")
	fmt.Println()
	fmt.Println("    To      :", mail.To)
	if strings.TrimSpace(mail.Cc) != "" {
		fmt.Println("    Cc      :", mail.Cc)
	}
	fmt.Println("    Subject :", mail.Subject)
	fmt.Println()
	fmt.Println("This lands in a real inbox. Check the addresses.")

	if !assumeYes {
		fmt.Print("Type yes to send: ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "yes" {
			fmt.Println("Nothing sent.")
			return 1
		}
	}

	var logf browser.Logf
	if verbose {
		logf = func(format string, args ...any) {
			fmt.Printf("   . "+format+"\n", args...)
		}
	}

	page, err := browser.Launch(context.Background(), browser.Options{
		ProfileDir: profileDir,
		URL:        url,
		ChromePath: chromePath,
		Log:        logf,
	})
	if err != nil {
		fmt.Println("Could not start the browser:", err)
		return 1
	}
	defer page.Leave()

	if browser.WaitForMailbox(page, 30*time.Minute, logf, func(m string) {
		fmt.Println("   ", m)
	}) == nil {
		fmt.Println("The inbox never opened - sign in, then run this again.")
		return 1
	}

	say := browser.Logf(func(format string, args ...any) {
		fmt.Printf("    "+format+"\n", args...)
	})
	if err := browser.SendTestMail(page, mail, say); err != nil {
		fmt.Println()
		fmt.Println("Not sent:", err)
		return 1
	}

	fmt.Println()
	fmt.Println("Sent. Give it a moment to arrive, then test the tool with:")
	fmt.Printf("    socfollowup.exe --diagnose SOC%s\n", mail.Case)
	return 0
}

// runDiagnose searches a real mailbox for one case and reports which link in
// the chain broke. It opens no mail, clicks nothing, and sends nothing.
func runDiagnose(num, url, profileDir, outDir, chromePath string, verbose bool) int {
	num = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(num)), "SOC")
	num = strings.TrimSpace(num)
	if num == "" {
		fmt.Println("--diagnose needs a case number, e.g. --diagnose 610529")
		return 2
	}

	var logf browser.Logf
	if verbose {
		logf = func(format string, args ...any) {
			fmt.Printf("   . "+format+"\n", args...)
		}
	}

	fmt.Println("Opening the mailbox. Sign in if asked - this only searches and")
	fmt.Println("reads the page. No mail is opened and nothing is sent.")
	fmt.Println()

	page, err := browser.Launch(context.Background(), browser.Options{
		ProfileDir: profileDir,
		URL:        url,
		ChromePath: chromePath,
		Log:        logf,
	})
	if err != nil {
		fmt.Println("Could not start the browser:", err)
		return 1
	}
	// Left open deliberately: the window is half the evidence.
	defer page.Leave()

	if browser.WaitForMailbox(page, 30*time.Minute, logf, func(m string) {
		fmt.Println("   ", m)
	}) == nil {
		fmt.Println("The inbox never opened - sign in, then run this again.")
		return 1
	}

	report := browser.Diagnose(page, num, 3*time.Second)
	fmt.Println()
	fmt.Println(report)

	name := filepath.Join(outDir, fmt.Sprintf("diagnose_SOC%s_%s.txt",
		num, time.Now().Format("20060102_150405")))
	if err := os.WriteFile(name, []byte(report), 0o644); err != nil {
		fmt.Println("(could not save the report:", err, ")")
		return 0
	}
	fmt.Println("Saved to:", name)
	return 0
}

// runCheck reads an export and reports what a run would do with it. Nothing
// touches Outlook.
func runCheck(sheet, column string) int {
	if sheet == "" {
		fmt.Println("--check needs --csv <export>")
		return 2
	}
	report, err := core.Preflight(sheet, column)
	if err != nil {
		fmt.Println("Could not read the export:", err)
		return 1
	}
	fmt.Println(core.FormatPreflight(report))
	return 0
}

func beside(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func confirm(prompt, want string) bool {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.TrimSpace(line) == want
}
