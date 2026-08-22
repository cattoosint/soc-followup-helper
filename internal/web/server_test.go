package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/core"
	"github.com/cattoosint/socfollowup-test/internal/fakeowa"
)

// The tracker is what an analyst actually clicks, and it can cause mail to be
// sent - so it is driven here the way the page drives it: real HTTP, real
// engine, real Chrome, against the stand-in mailbox.

type harness struct {
	srv   *Server
	base  string
	token string
}

func start(t *testing.T, d Defaults) *harness {
	t.Helper()
	srv := New(d)
	url, err := srv.Serve(context.Background(), false) // never open a browser in a test
	if err != nil {
		t.Fatalf("starting the tracker: %v", err)
	}
	t.Cleanup(srv.Shutdown)
	base := strings.SplitN(url, "/?t=", 2)[0]
	return &harness{srv: srv, base: base, token: srv.token}
}

func (h *harness) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", h.base+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Socfu-Token", h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) state(t *testing.T) state {
	t.Helper()
	req, err := http.NewRequest("GET", h.base+"/api/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Socfu-Token", h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st state
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func writeSheet(t *testing.T, dir string, cases ...string) string {
	t.Helper()
	body := "Case ID,Summary\n"
	for _, c := range cases {
		body += c + ",alert " + c + "\n"
	}
	path := filepath.Join(dir, "shift.csv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Another process on this machine must not be able to drive a page that can
// send mail.
func TestUntokenedRequestsAreRefused(t *testing.T) {
	h := start(t, Defaults{})

	for _, path := range []string{"/api/state", "/api/start", "/api/stop", "/"} {
		resp, err := http.Get(h.base + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s returned %d without a token, want 403",
				path, resp.StatusCode)
		}
	}
}

// Auto-send needs the acknowledgement every run, not once.
func TestAutoSendRefusedWithoutAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	h := start(t, Defaults{OutputDir: dir, ProfileDir: filepath.Join(dir, "p")})
	sheet := writeSheet(t, dir, "610529")

	code, out := h.post(t, "/api/start", startRequest{
		Sheet: sheet, AutoSend: true, AutoSender: "jordan@example.com",
		Agreed: false,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("started with no acknowledgement (status %d)", code)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "acknowledgement") {
		t.Errorf("error was %q, want it to name the acknowledgement", msg)
	}

	code, out = h.post(t, "/api/start", startRequest{
		Sheet: sheet, AutoSend: true, AutoSender: "", Agreed: true,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("started auto-send with no sender (status %d)", code)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "sender") {
		t.Errorf("error was %q, want it to name the sender", msg)
	}
}

func TestBadExportIsReportedNotStarted(t *testing.T) {
	dir := t.TempDir()
	h := start(t, Defaults{OutputDir: dir})

	code, out := h.post(t, "/api/start", startRequest{Sheet: ""})
	if code != http.StatusBadRequest {
		t.Fatalf("started with no export (status %d)", code)
	}
	if _, ok := out["error"]; !ok {
		t.Error("no error reported for a missing export")
	}

	code, _ = h.post(t, "/api/start", startRequest{
		Sheet: filepath.Join(dir, "does-not-exist.csv")})
	if code != http.StatusBadRequest {
		t.Fatalf("started with a missing file (status %d)", code)
	}
}

func TestCheckReportsWithoutTouchingOutlook(t *testing.T) {
	dir := t.TempDir()
	h := start(t, Defaults{OutputDir: dir})
	sheet := writeSheet(t, dir, "610529", "700001")

	code, out := h.post(t, "/api/check", map[string]string{"sheet": sheet})
	if code != http.StatusOK {
		t.Fatalf("check returned %d", code)
	}
	report, _ := out["report"].(string)
	if !strings.Contains(report, "Cases to process   : 2") {
		t.Errorf("report did not describe the export:\n%s", report)
	}
}

// A whole run driven through the page, answering the prompt the way the
// analyst would.
func TestRunThroughTheTracker(t *testing.T) {
	if os.Getenv("SOCFU_SKIP_BROWSER") != "" {
		t.Skip("browser tests disabled")
	}

	mailbox := fakeowa.Start()
	defer mailbox.Close()

	dir := t.TempDir()
	h := start(t, Defaults{
		URL:        mailbox.URL,
		ProfileDir: filepath.Join(dir, "profile"),
		OutputDir:  dir,
	})
	sheet := writeSheet(t, dir, "610529", "700001", "999999")

	code, out := h.post(t, "/api/start", startRequest{
		Sheet:      sheet,
		URL:        mailbox.URL,
		Settle:     "0.3",
		SendDelay:  "0",
		NoPause:    true,
		Headless:   true,
		AutoSend:   true,
		AutoSender: "jordan@example.com",
		AutoPhrase: "follow up",
		Agreed:     true,
	})
	if code != http.StatusOK {
		t.Fatalf("start returned %d: %v", code, out)
	}
	if n, _ := out["cases"].(float64); int(n) != 3 {
		t.Fatalf("started with %v cases, want 3", out["cases"])
	}

	// Poll the way the page does, and click Skip whenever it asks.
	deadline := time.Now().Add(3 * time.Minute)
	var st state
	for time.Now().Before(deadline) {
		st = h.state(t)
		if st.Prompt.Active {
			h.post(t, "/api/answer", map[string]string{"choice": "s"})
		}
		if st.Done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !st.Done {
		t.Fatalf("run did not finish:\n%s", strings.Join(st.Logs, "\n"))
	}
	if st.Error != "" {
		t.Fatalf("run reported: %s", st.Error)
	}

	got := map[string]string{}
	for _, c := range st.Cases {
		got[c.Num] = c.Status
	}
	want := map[string]string{
		"610529": core.StatusSent,     // genuine sender: auto-sent
		"700001": core.StatusSkipped,  // quoted sender: handed back, skipped
		"999999": core.StatusNotFound, // no mail: flagged
	}
	for num, wantStatus := range want {
		if got[num] != wantStatus {
			t.Errorf("SOC%s showed %q in the tracker, want %q",
				num, got[num], wantStatus)
		}
	}

	if st.Replied != 1 || st.NotFound != 1 || st.Skipped != 1 {
		t.Errorf("tiles showed %d replied / %d not found / %d skipped; want 1/1/1",
			st.Replied, st.NotFound, st.Skipped)
	}
	if len(st.Files) != 2 {
		t.Errorf("run wrote %v, want a results CSV and a review sheet", st.Files)
	}
	if len(st.Logs) == 0 {
		t.Error("the log pane would have been empty")
	}
}
