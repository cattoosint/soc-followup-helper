// Package web serves the analyst's tracker as a local page.
//
// Go has no tkinter, and a native toolkit would mean another dependency and a
// C compiler. Serving the window over loopback keeps the binary self-contained
// and gives the analyst the same thing the Python build's window gave: a live
// case table, the running log, and the buttons that answer the engine.
//
// It listens on 127.0.0.1 only, and every request must carry a token minted at
// startup - this page can cause mail to be sent, so another process on the
// same machine must not be able to drive it.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/browser"
	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/engine"
)

// caseRow is one line of the tracker.
type caseRow struct {
	Num    string `json:"num"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// promptState is the question the engine is currently blocked on.
type promptState struct {
	Text    string   `json:"text"`
	Choices []string `json:"choices"`
	Kind    string   `json:"kind"`
	Active  bool     `json:"active"`
	// Gen identifies WHICH prompt this is. A click carries the generation the
	// page was showing, so an answer aimed at a prompt that has already gone
	// cannot be applied to the next one.
	Gen uint64 `json:"gen"`
}

type state struct {
	Running  bool        `json:"running"`
	Done     bool        `json:"done"`
	Error    string      `json:"error"`
	Cases    []caseRow   `json:"cases"`
	Logs     []string    `json:"logs"`
	Prompt   promptState `json:"prompt"`
	Replied  int         `json:"replied"`
	NotFound int         `json:"notFound"`
	Errored  int         `json:"errored"`
	Skipped  int         `json:"skipped"`
	Total    int         `json:"total"`
	Files    []string    `json:"files"`
}

// Server is the tracker. It implements engine.UI.
type Server struct {
	// promptGen names the current prompt, so a late click cannot answer the
	// next one. Guarded by mu.
	promptGen uint64

	mu      sync.Mutex
	st      state
	index   map[string]int
	answers chan string
	stop    bool
	done    chan struct{}

	token   string
	opts    Defaults
	httpSrv *http.Server
}

// Defaults seed the settings form.
type Defaults struct {
	URL        string
	ProfileDir string
	OutputDir  string
	AutoSender string
	AutoPhrase string
	ChromePath string
}

// New builds a tracker server.
func New(d Defaults) *Server {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return &Server{
		index:   map[string]int{},
		answers: make(chan string, 1),
		done:    make(chan struct{}),
		token:   hex.EncodeToString(buf),
		opts:    d,
	}
}

// --- engine.UI ------------------------------------------------------------

func (s *Server) Log(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range strings.Split(msg, "\n") {
		s.st.Logs = append(s.st.Logs, line)
	}
	// keep the page light on a 300-case shift
	if len(s.st.Logs) > 2000 {
		s.st.Logs = s.st.Logs[len(s.st.Logs)-2000:]
	}
}

func (s *Server) CaseUpdate(num, status, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.index[num]; ok {
		s.st.Cases[i].Status = status
		s.st.Cases[i].Detail = detail
	} else {
		s.index[num] = len(s.st.Cases)
		s.st.Cases = append(s.st.Cases, caseRow{num, status, detail})
	}
	s.recount()
}

// recount refreshes the tallies. Caller holds the lock.
func (s *Server) recount() {
	s.st.Replied, s.st.NotFound, s.st.Errored, s.st.Skipped = 0, 0, 0, 0
	for _, c := range s.st.Cases {
		switch c.Status {
		case core.StatusSent:
			s.st.Replied++
		case core.StatusSentUnverified:
			// deliberately NOT counted as replied: nobody confirmed it left
			s.st.Skipped++
		case core.StatusNotFound:
			s.st.NotFound++
		case core.StatusError:
			s.st.Errored++
		case core.StatusSkipped:
			s.st.Skipped++
		}
	}
}

func (s *Server) Ask(prompt string, choices []string, kind string) string {
	s.setPrompt(prompt, choices, kind)
	defer s.clearPrompt()
	for {
		select {
		case a := <-s.answers:
			if allowed(a, choices) {
				return a
			}
		case <-s.done:
			return "q"
		}
	}
}

// AskOrWatch is Ask, but gives up the moment the draft closes on its own -
// that is the analyst having pressed Send in Outlook, which is what they will
// actually do rather than come back to this page.
func (s *Server) AskOrWatch(prompt string, choices []string, kind string,
	watch func() bool) string {

	s.setPrompt(prompt, choices, kind)
	defer s.clearPrompt()

	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case a := <-s.answers:
			if allowed(a, choices) {
				return a
			}
		case <-tick.C:
			if watch != nil && watch() {
				return "auto"
			}
		case <-s.done:
			return "q"
		}
	}
}

func (s *Server) StopRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop
}

// setPrompt shows a prompt and starts a new generation.
//
// It also drains any answer left in the buffer. The channel holds one, and a
// non-blocking send SUCCEEDS when nobody is waiting - so a click that arrived
// a moment after the engine moved on used to sit there and be consumed by the
// NEXT prompt. That silently answered "was it sent?" with a stale yes, and
// painted a green row nobody had confirmed.
func (s *Server) setPrompt(text string, choices []string, kind string) uint64 {
	for {
		select {
		case stale := <-s.answers:
			_ = stale
			continue
		default:
		}
		break
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptGen++
	s.st.Prompt = promptState{Text: text, Choices: choices, Kind: kind,
		Active: true, Gen: s.promptGen}
	return s.promptGen
}

func (s *Server) clearPrompt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Prompt = promptState{}
}

func allowed(answer string, choices []string) bool {
	for _, c := range choices {
		if answer == c {
			return true
		}
	}
	return false
}

// --- HTTP -----------------------------------------------------------------

// Serve starts the tracker and blocks until the browser tab asks it to quit.
func (s *Server) Serve(ctx context.Context, open bool) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, s.token)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.guard(s.handleState))
	mux.HandleFunc("/api/start", s.guard(s.handleStart))
	mux.HandleFunc("/api/answer", s.guard(s.handleAnswer))
	mux.HandleFunc("/api/stop", s.guard(s.handleStop))
	mux.HandleFunc("/api/check", s.guard(s.handleCheck))
	mux.HandleFunc("/api/browse", s.guard(s.handleBrowse))
	mux.HandleFunc("/api/upload", s.guard(s.handleUpload))

	s.httpSrv = &http.Server{Handler: mux}
	go func() { _ = s.httpSrv.Serve(ln) }()

	if open {
		openBrowser(url)
	}
	return url, nil
}

// Shutdown stops the server.
func (s *Server) Shutdown() {
	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(ctx)
	}
}

// guard rejects anything without the token minted at startup.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Socfu-Token") != s.token &&
			r.URL.Query().Get("t") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("t") != s.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.NewReplacer(
		"__TOKEN__", s.token,
		"__URL__", s.opts.URL,
		"__SENDER__", s.opts.AutoSender,
		"__PHRASE__", s.opts.AutoPhrase,
	).Replace(indexHTML)
	_, _ = w.Write([]byte(page))
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	// Encoded while the lock is held. Copying the struct copies a slice
	// HEADER, so CaseUpdate went on writing into the same backing array as the
	// encoder walked it - and the page was served a row whose status came from
	// one update and whose detail came from another.
	s.mu.Lock()
	// A nil slice marshals to JSON null, and the page reads .length off all
	// three of these. Only Cases was ever fixed, and only on the start path,
	// so a fresh state - what the page sees before the first run, and after a
	// run that died - still served nulls. Normalise all of them here, where
	// every response goes through.
	snapshot := s.st
	if snapshot.Cases == nil {
		snapshot.Cases = []caseRow{}
	}
	if snapshot.Logs == nil {
		snapshot.Logs = []string{}
	}
	if snapshot.Files == nil {
		snapshot.Files = []string{}
	}
	buf, err := json.Marshal(snapshot)
	s.mu.Unlock()
	if err != nil {
		writeErr(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf)
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Choice string `json:"choice"`
		Gen    uint64 `json:"gen"`
	}
	if err := readRequest(r, &body); err != nil {
		writeErr(w, err.Error())
		return
	}
	// An answer names the prompt it was aimed at. A click that lands after the
	// engine has moved on - the draft-close watcher fires on its own, so this
	// is routine, not an edge case - is discarded rather than applied to
	// whatever is being asked next.
	s.mu.Lock()
	active, current := s.st.Prompt.Active, s.st.Prompt.Gen
	s.mu.Unlock()
	if !active || (body.Gen != 0 && body.Gen != current) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	select {
	case s.answers <- body.Choice:
	default: // nothing is waiting; drop it rather than block the page
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.stop = true
	s.mu.Unlock()
	s.Log("    Stop requested - finishing the current case.")
	select {
	case s.answers <- "q":
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sheet  string `json:"sheet"`
		Column string `json:"column"`
	}
	if err := readRequest(r, &body); err != nil {
		writeErr(w, err.Error())
		return
	}
	report, err := core.Preflight(strings.TrimSpace(body.Sheet), body.Column)
	out := map[string]string{}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["report"] = core.FormatPreflight(report)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// readRequest decodes a JSON body and explains itself when it cannot.
//
// "bad request" with no detail is useless to an analyst on a locked-down
// machine, where something between the page and the tool may be interfering
// with the request. Say what actually arrived.
func readRequest(r *http.Request, out any) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("could not read the request: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("the request arrived with an empty body - something " +
			"between the browser and this tool is stripping it")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		shown := string(raw)
		if len(shown) > 200 {
			shown = shown[:200] + "..."
		}
		return fmt.Errorf("could not read the request body (%d bytes): %w; "+
			"it began %q", len(raw), err, shown)
	}
	return nil
}

// handleBrowse opens the ordinary Windows file dialog, host-side, and returns
// the real path. A browser will not give a page the path of a chosen file.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path, err := chooseFile()
	out := map[string]string{}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["path"] = path // "" means the analyst cancelled
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// maxUpload bounds a dropped file. A shift export is a few hundred rows; this
// is generous, and stops a stray drop filling the disk.
const maxUpload = 64 << 20 // 64 MB

// handleUpload takes a file dragged onto the page and writes it somewhere the
// engine can read. Used when the native dialog is unavailable, and because
// dragging the export out of Downloads is quicker than any dialog.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		// Every failure used to be reported as "too big", discarding the real
		// error - the exact thing readRequest was written to stop doing.
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, "that file is too big to drop here - use Browse instead")
			return
		}
		writeErr(w, "could not read the dropped file: "+err.Error())
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("sheet")
	if err != nil {
		writeErr(w, "no file came through")
		return
	}
	defer file.Close()

	// Base() so a crafted filename cannot write outside the temp directory.
	name := filepath.Base(header.Filename)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "dropped-export"
	}
	dir, err := os.MkdirTemp("", "socfu_drop_")
	if err != nil {
		writeErr(w, err.Error())
		return
	}
	dst := filepath.Join(dir, name)
	out, err := os.Create(dst)
	if err != nil {
		writeErr(w, err.Error())
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeErr(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": dst, "name": name})
}

// startRequest is what the settings form sends.
type startRequest struct {
	Sheet      string `json:"sheet"`
	Column     string `json:"column"`
	URL        string `json:"url"`
	Limit      string `json:"limit"`
	Settle     string `json:"settle"`
	SendDelay  string `json:"sendDelay"`
	NoPause    bool   `json:"noPause"`
	Headless   bool   `json:"headless"`
	AutoSend   bool   `json:"autoSend"`
	AutoSender string `json:"autoSender"`
	AutoPhrase string `json:"autoPhrase"`
	Agreed     bool   `json:"agreed"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := readRequest(r, &req); err != nil {
		writeErr(w, err.Error())
		return
	}

	s.mu.Lock()
	if s.st.Running {
		s.mu.Unlock()
		writeErr(w, "a run is already going")
		return
	}
	s.mu.Unlock()

	sheet := strings.TrimSpace(req.Sheet)
	if sheet == "" {
		writeErr(w, "point me at an export first")
		return
	}
	// Auto-send puts mail in front of real recipients with no human check, so
	// it needs a sender AND an explicit acknowledgement, every run.
	if req.AutoSend {
		if strings.TrimSpace(req.AutoSender) == "" {
			writeErr(w, "auto-send needs the sender the newest mail must be from")
			return
		}
		if !req.Agreed {
			writeErr(w, "tick the auto-send acknowledgement first")
			return
		}
	}

	// Claim the run BEFORE the slow work. The check and the set used to sit
	// either side of ExtractCases, so two starts could both be accepted: two
	// engines on one profile, the second's stale-profile guard killing the
	// first's browser mid-case, and the page reporting "finished" while the
	// second was still driving Outlook.
	s.mu.Lock()
	if s.st.Running {
		s.mu.Unlock()
		writeErr(w, "a run is already going")
		return
	}
	s.st.Running = true
	s.mu.Unlock()

	release := func() {
		s.mu.Lock()
		s.st.Running = false
		s.mu.Unlock()
	}

	column, cases, err := core.ExtractCases(sheet, strings.TrimSpace(req.Column), nil)
	if err != nil {
		release()
		writeErr(w, err.Error())
		return
	}
	if n := atoiOr(req.Limit, 0); n > 0 && n < len(cases) {
		cases = cases[:n]
	}
	if len(cases) == 0 {
		release()
		writeErr(w, "no case numbers in that export")
		return
	}

	s.mu.Lock()
	// Cases starts as an empty slice, never nil: it is serialised straight to
	// the page, and a JSON null there threw before anything could be drawn -
	// so a run that died during sign-in showed the analyst a blank tracker.
	s.st = state{Running: true, Total: len(cases), Cases: []caseRow{}}
	s.index = map[string]int{}
	s.stop = false
	s.mu.Unlock()

	opts := engine.Options{
		URL:         firstNonEmpty(strings.TrimSpace(req.URL), s.opts.URL),
		ProfileDir:  s.opts.ProfileDir,
		OutputDir:   s.opts.OutputDir,
		Settle:      seconds(req.Settle, 3),
		SendDelay:   seconds(req.SendDelay, 5),
		AutoSend:    req.AutoSend,
		AutoKeyword: firstNonEmpty(strings.TrimSpace(req.AutoPhrase), "follow up"),
		AutoSender:  strings.TrimSpace(req.AutoSender),
		NoPause:     req.NoPause,
		Headless:    req.Headless,
		ChromePath:  s.opts.ChromePath,
	}

	go s.run(sheet, column, cases, opts)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "cases": len(cases), "column": column,
	})
}

func (s *Server) run(sheet, column string, cases []string, opts engine.Options) {
	logf := browser.Logf(func(format string, args ...any) {
		s.Log("      " + fmt.Sprintf(format, args...))
	})

	summary := engine.Run(context.Background(), sheet, column, cases, opts, s, logf)

	s.mu.Lock()
	s.st.Running = false
	s.st.Done = true
	if summary.RunError != nil {
		s.st.Error = summary.RunError.Error()
	}
	for _, p := range []string{summary.CSVPath, summary.XLSXPath} {
		if p != "" {
			s.st.Files = append(s.st.Files, filepath.Base(p))
		}
	}
	s.mu.Unlock()
}

func writeErr(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

func seconds(s string, fallback float64) time.Duration {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		v = fallback
	}
	return time.Duration(v * float64(time.Second))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// openBrowser shows the tracker in the analyst's normal browser - deliberately
// not the automated one, which is busy driving Outlook.
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
