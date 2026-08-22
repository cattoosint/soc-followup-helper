package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cattoosint/socfollowup-test/internal/core"
)

// dropFile posts a file the way the page does when one is dragged onto it.
func (h *harness) dropFile(t *testing.T, name string, body []byte) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("sheet", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", h.base+"/api/upload", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
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

// A dragged export has to end up somewhere the engine can actually read.
func TestDroppedFileIsReadableByTheEngine(t *testing.T) {
	h := start(t, Defaults{})

	code, out := h.dropFile(t, "shift.csv",
		[]byte("Case ID,Summary\n610529,suspicious login\n700001,quoted thread\n"))
	if code != http.StatusOK {
		t.Fatalf("upload returned %d: %v", code, out)
	}
	path, _ := out["path"].(string)
	if path == "" {
		t.Fatal("upload returned no path")
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })

	_, cases, err := core.ExtractCases(path, "", nil)
	if err != nil {
		t.Fatalf("engine could not read the dropped file: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("read %v from the dropped file, want 2 cases", cases)
	}
}

// The filename comes from the browser, so it is not trusted to stay inside the
// directory we chose for it.
func TestDroppedFilenameCannotEscapeItsDirectory(t *testing.T) {
	h := start(t, Defaults{})

	for _, name := range []string{
		"../escaped.csv",
		"..\\escaped.csv",
		"../../../../escaped.csv",
	} {
		code, out := h.dropFile(t, name, []byte("Case ID\n610529\n"))
		if code != http.StatusOK {
			t.Fatalf("%q: upload returned %d: %v", name, code, out)
		}
		path, _ := out["path"].(string)
		if path == "" {
			t.Fatalf("%q: no path returned", name)
		}
		t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })

		dir := filepath.Dir(path)
		if !strings.HasPrefix(filepath.Base(dir), "socfu_drop_") {
			t.Errorf("%q escaped to %q", name, path)
		}
		if strings.Contains(filepath.Base(path), "..") {
			t.Errorf("%q kept a traversal in its name: %q", name, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%q: nothing written: %v", name, err)
		}
	}
}

func TestEmptyDropIsReported(t *testing.T) {
	h := start(t, Defaults{})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.Close()

	req, _ := http.NewRequest("POST", h.base+"/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Socfu-Token", h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an empty drop returned %d, want 400", resp.StatusCode)
	}
}

// A CSV loses the Excel filter, so every row in it gets followed up. The check
// report has to make that visible rather than quietly processing rows the
// analyst had filtered out.
func TestCSVHasNoFilteredRowsToSkip(t *testing.T) {
	dir := t.TempDir()
	h := start(t, Defaults{OutputDir: dir})

	path := filepath.Join(dir, "unfiltered.csv")
	body := "Case ID,Summary\n610529,a\n700001,b\n999999,c\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := h.post(t, "/api/check", map[string]string{"sheet": path})
	if code != http.StatusOK {
		t.Fatalf("check returned %d", code)
	}
	report, _ := out["report"].(string)
	if !strings.Contains(report, "Cases to process   : 3") {
		t.Errorf("all three rows should be processed from a CSV:\n%s", report)
	}
	// An xlsx reports what the filter hid; a CSV has nothing to report, which
	// is exactly why the page warns about it.
	if strings.Contains(report, "Filtered out (Excel)") {
		t.Errorf("a CSV cannot carry a filter, but the report claimed one:\n%s",
			report)
	}
}
