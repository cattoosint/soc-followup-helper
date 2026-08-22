package main

import (
	"bufio"
	"fmt"
	"strings"
	"sync"
	"time"
)

// consoleUI is the terminal front-end. The engine never knows which front-end
// it has - it only calls these five methods.
type consoleUI struct {
	in   *bufio.Reader
	mu   sync.Mutex
	stop bool

	// One reader goroutine owns stdin for the life of the run, feeding lines.
	// AskOrWatch used to start its own and could return "auto" while that
	// goroutine was still blocked in ReadString; Ask then read the same
	// bufio.Reader from the main goroutine. Two concurrent readers is a data
	// race, and in practice the orphan swallowed the analyst's next answer -
	// so "s" (no, it was not sent) vanished and the following Enter was read
	// as "yes", turning the row green.
	readerOnce sync.Once
	lines      chan string
	// lastWasAuto records that the previous question was answered by the
	// draft-closed watcher rather than by the analyst. That is the only case
	// where a keystroke can still be in flight for a question that has gone,
	// so it is the only case worth draining - draining every time threw away
	// answers the analyst had already typed.
	lastWasAuto bool
}

// stdinLines returns the shared line channel, starting the single reader the
// first time one is needed. The channel closes when stdin does.
func (u *consoleUI) stdinLines() chan string {
	u.readerOnce.Do(func() {
		u.lines = make(chan string, 1)
		go func() {
			defer close(u.lines)
			for {
				line, err := u.in.ReadString('\n')
				if err != nil {
					return
				}
				u.lines <- strings.ToLower(strings.TrimSpace(line))
			}
		}()
	})
	return u.lines
}

// drainStdin throws away anything typed before this prompt appeared, so a
// keypress aimed at a question that has already been answered - by the
// draft-closed watcher, say - cannot answer the next one.
func (u *consoleUI) drainStdin() {
	lines := u.stdinLines()
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// stopAndQuit records that no more answers can come. "q" is never a blind yes.
func (u *consoleUI) stopAndQuit() string {
	u.mu.Lock()
	u.stop = true
	u.mu.Unlock()
	return "q"
}

func (u *consoleUI) Log(msg string) { fmt.Println(msg) }

func (u *consoleUI) CaseUpdate(num, status, detail string) {
	// The tracker is a table in the window front-end; on a terminal the
	// running commentary already says everything, so only real outcomes are
	// worth a line of their own.
	switch status {
	case "WORKING", "REVIEW":
		return
	}
	fmt.Printf("    -> SOC%s: %s\n", num, status)
}

func (u *consoleUI) Ask(prompt string, choices []string, kind string) string {
	lines := u.stdinLines()
	if u.lastWasAuto {
		u.drainStdin()
		u.lastWasAuto = false
	}
	for {
		fmt.Print(prompt)
		answer, ok := <-lines
		if !ok {
			// stdin closed - treat it as "stop", never as a blind yes
			return u.stopAndQuit()
		}
		for _, c := range choices {
			if answer == c {
				return answer
			}
		}
		fmt.Println("    (please answer one of: " +
			strings.Join(displayChoices(choices), ", ") + ")")
	}
}

// AskOrWatch waits for the analyst, but gives up the moment the draft closes
// on its own - that is the analyst having pressed Send in the browser, which
// is what they will actually do.
func (u *consoleUI) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {

	lines := u.stdinLines()
	if u.lastWasAuto {
		u.drainStdin()
		u.lastWasAuto = false
	}
	fmt.Print(prompt)

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case answer, ok := <-lines:
			if !ok {
				return u.stopAndQuit()
			}
			for _, c := range choices {
				if answer == c {
					return answer
				}
			}
			// An unrecognised answer must not be read as "sent". Ask again on
			// the blocking path, where the reply is unambiguous.
			return u.Ask(prompt, choices, kind)
		case <-ticker.C:
			if watch != nil && watch() {
				fmt.Println()
				u.lastWasAuto = true
				return "auto"
			}
		}
	}
}

func (u *consoleUI) StopRequested() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stop
}

func displayChoices(choices []string) []string {
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		if c == "" {
			out = append(out, "Enter")
			continue
		}
		out = append(out, c)
	}
	return out
}

// scriptUI answers every prompt the same way. The self-test uses it so a run
// completes with nobody at the keyboard.
type scriptUI struct {
	answer string
	quiet  bool
}

func (u *scriptUI) Log(msg string) {
	if !u.quiet {
		fmt.Println(msg)
	}
}

func (u *scriptUI) CaseUpdate(num, status, detail string) {}

func (u *scriptUI) Ask(prompt string, choices []string, kind string) string {
	return u.answer
}

func (u *scriptUI) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {
	return u.answer
}

func (u *scriptUI) StopRequested() bool { return false }
