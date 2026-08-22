// Package cdp is a minimal Chrome DevTools Protocol client.
//
// It replaces the chromedp library. Not for fun: chromedp ships ten
// JavaScript files that it pulls in with //go:embed, and a mail gateway will
// not carry a `.js` file even inside an archive - which made the vendored
// bundle impossible to get onto the machine this tool exists for. Everything
// here is Go, so the bundle became plain source that arrives intact.
//
// Only what the follow-up tool actually needs is implemented: launch Chrome,
// attach to the page, evaluate JavaScript, send keys and clicks, take a
// screenshot. It is deliberately small enough to read in one sitting.
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options configures a browser launch.
type Options struct {
	// ChromePath is optional; standard install locations are searched when empty.
	ChromePath string
	ProfileDir string
	Headless   bool
	// ExtraArgs are appended to the command line.
	ExtraArgs []string
}

// Browser is a running Chrome with one page attached.
type Browser struct {
	cmd     *exec.Cmd
	ws      *wsConn
	userDir string

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	waiting map[int64]chan *response
	closed  bool
	readErr error
}

type request struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *protocolError  `json:"error"`
	Method string          `json:"method"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (e *protocolError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Data)
	}
	return e.Message
}

// Launch starts Chrome and attaches to its first page.
func Launch(ctx context.Context, opts Options) (*Browser, error) {
	exe := opts.ChromePath
	if exe == "" {
		found, err := FindChrome()
		if err != nil {
			return nil, err
		}
		exe = found
	}

	userDir := opts.ProfileDir
	if userDir == "" {
		tmp, err := os.MkdirTemp("", "socfu_chrome_")
		if err != nil {
			return nil, err
		}
		userDir = tmp
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating profile dir: %w", err)
	}
	abs, err := filepath.Abs(userDir)
	if err != nil {
		return nil, err
	}

	args := []string{
		// port 0 means "pick a free one and write it to DevToolsActivePort"
		"--remote-debugging-port=0",
		"--user-data-dir=" + abs,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-features=Translate,MediaRouter",
		"--disable-blink-features=AutomationControlled",
		"--password-store=basic",
		"--no-service-autorun",
		"--homepage=about:blank",
		// Wide enough that OWA lays out the full three-column view. On a
		// narrow window it collapses the folder list, and the tool then
		// cannot reach Sent Items at all - so every send comes back "could
		// not check" and a confirmed green is unreachable. It also hides
		// Reply all behind the "..." menu.
		"--window-size=1500,950",
		"about:blank",
	}
	if opts.Headless {
		args = append([]string{"--headless=new", "--disable-gpu"}, args...)
	} else {
		args = append([]string{"--start-maximized"}, args...)
	}
	args = append(args, opts.ExtraArgs...)

	// The port file is stale until this launch rewrites it.
	portFile := filepath.Join(abs, "DevToolsActivePort")
	_ = os.Remove(portFile)

	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting Chrome: %w", err)
	}

	b := &Browser{cmd: cmd, userDir: abs, waiting: map[int64]chan *response{}}

	port, err := waitForPort(ctx, portFile, 60*time.Second)
	if err != nil {
		b.Close()
		return nil, err
	}
	wsURL, err := pageWebSocket(ctx, port, 30*time.Second)
	if err != nil {
		b.Close()
		return nil, err
	}

	conn, err := wsDial(wsURL, 20*time.Second)
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("connecting to Chrome: %w", err)
	}
	b.ws = conn

	go b.readLoop()

	// Page events are not consumed, but enabling the domains makes navigation
	// and evaluation behave predictably.
	for _, m := range []string{"Page.enable", "Runtime.enable", "DOM.enable"} {
		if err := b.Send(ctx, m, nil, nil); err != nil {
			b.Close()
			return nil, fmt.Errorf("%s: %w", m, err)
		}
	}
	return b, nil
}

// waitForPort reads the port Chrome chose out of DevToolsActivePort.
func waitForPort(ctx context.Context, portFile string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(portFile); err == nil {
			line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
			if port, err := strconv.Atoi(line); err == nil && port > 0 {
				return port, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return 0, errors.New("Chrome did not report a debugging port - it may be " +
		"blocked from starting, or another Chrome is holding this profile")
}

type targetInfo struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// pageWebSocket finds the page target to drive.
//
// Attaching straight to the page's own endpoint means every command is already
// page-scoped: no Target.attachToTarget, no session ids to thread through.
func pageWebSocket(ctx context.Context, port int, timeout time.Duration) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/list", port)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err == nil {
			var targets []targetInfo
			decErr := json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
			if decErr == nil {
				for _, t := range targets {
					if t.Type == "page" && t.WebSocketDebuggerURL != "" {
						return t.WebSocketDebuggerURL, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return "", errors.New("Chrome started but never offered a page to drive")
}

// readLoop dispatches replies to whoever is waiting for them. Events are
// ignored: nothing here needs them.
func (b *Browser) readLoop() {
	for {
		payload, err := b.ws.ReadMessage()
		if err != nil {
			b.fail(err)
			return
		}
		var msg response
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue // not something we asked for
		}
		if msg.ID == 0 {
			continue // an event
		}
		b.mu.Lock()
		ch, ok := b.waiting[msg.ID]
		delete(b.waiting, msg.ID)
		b.mu.Unlock()
		if ok {
			ch <- &msg
		}
	}
}

// fail wakes every waiter when the connection dies, so no call hangs forever.
func (b *Browser) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.readErr == nil {
		b.readErr = err
	}
	for id, ch := range b.waiting {
		close(ch)
		delete(b.waiting, id)
	}
}

// Send issues one command and decodes its result into out (which may be nil).
func (b *Browser) Send(ctx context.Context, method string, params, out any) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("browser is closed")
	}
	if b.readErr != nil {
		err := b.readErr
		b.mu.Unlock()
		return fmt.Errorf("connection to Chrome lost: %w", err)
	}
	b.nextID++
	id := b.nextID
	ch := make(chan *response, 1)
	b.waiting[id] = ch
	b.mu.Unlock()

	payload, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}

	b.writeMu.Lock()
	err = b.ws.WriteText(payload)
	b.writeMu.Unlock()
	if err != nil {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return ctx.Err()
	case msg, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: connection to Chrome closed", method)
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %w", method, msg.Error)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

// Close ends the browser. Chrome is asked politely first, then killed.
// Detach drops the connection and leaves the browser running.
//
// When a run ends badly the window on screen is the only record of what the
// tool was looking at. Closing it takes that away at exactly the moment it is
// needed, so a run with anything to inspect detaches instead of closing.
func (b *Browser) Detach() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()

	if b.ws != nil {
		_ = b.ws.Close() // no Browser.close: the window stays up
	}
	// b.cmd is deliberately not waited on or killed. The next run's profile
	// guard will free this profile if it is still held.
}

func (b *Browser) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()

	if b.ws != nil {
		_ = b.sendNoWait("Browser.close")
		_ = b.ws.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		done := make(chan struct{})
		go func() { _, _ = b.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = b.cmd.Process.Kill()
		}
	}
}

// sendNoWait fires a command without caring about the reply, for shutdown.
func (b *Browser) sendNoWait(method string) error {
	payload, err := json.Marshal(request{ID: -1, Method: method})
	if err != nil {
		return err
	}
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.ws.WriteText(payload)
}
