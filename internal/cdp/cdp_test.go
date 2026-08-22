package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// These prove the hand-written DevTools client actually drives Chrome, which
// is the part chromedp used to do.

const testPage = `<!doctype html>
<html><body>
  <input id="box" value="start">
  <div id="out">nothing</div>
  <button id="btn" onclick="document.getElementById('out').textContent='clicked'">Press</button>
  <script>
    document.getElementById("box").addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        document.getElementById("out").textContent = "entered:" + this.value;
      }
    });
  </script>
</body></html>`

func launch(t *testing.T) (*Browser, *httptest.Server) {
	t.Helper()
	if os.Getenv("SOCFU_SKIP_BROWSER") != "" {
		t.Skip("browser tests disabled")
	}
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(testPage))
		}))

	dir, err := os.MkdirTemp("", "cdp_test_")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	b, err := Launch(ctx, Options{ProfileDir: dir, Headless: true})
	if err != nil {
		srv.Close()
		os.RemoveAll(dir)
		// Skipping here once hid a real defect - a wrong constant in the
		// handshake - behind a green run. Only a machine with no browser at
		// all gets to skip; if Chrome is present and will not drive, that is
		// a failure.
		if _, findErr := FindChrome(); findErr != nil {
			t.Skipf("no browser on this machine: %v", findErr)
		}
		t.Fatalf("Chrome is installed but could not be driven: %v", err)
	}
	t.Cleanup(func() {
		b.Close()
		srv.Close()
		os.RemoveAll(dir)
	})
	return b, srv
}

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestFindsAChromeToDrive(t *testing.T) {
	path, err := FindChrome()
	if err != nil {
		t.Skipf("no browser on this machine: %v", err)
	}
	if path == "" {
		t.Fatal("FindChrome returned an empty path with no error")
	}
	t.Logf("found %s", path)
}

func TestNavigateAndEvaluate(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)

	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	var title string
	if err := b.Eval(ctx, "document.getElementById('out').textContent", &title); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if title != "nothing" {
		t.Fatalf("read %q from the page, want %q", title, "nothing")
	}

	url, err := b.CurrentURL(ctx)
	if err != nil || !strings.HasPrefix(url, srv.URL) {
		t.Fatalf("CurrentURL = %q, %v", url, err)
	}
}

func TestEvalReportsScriptErrors(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)
	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	// A failure must surface, not be read as an empty result - the browser
	// layer decides whether to act on what it read.
	if err := b.Eval(ctx, "throw new Error('boom')", nil); err == nil {
		t.Fatal("a throwing script reported success")
	}
}

func TestTypingAndKeys(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)
	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}

	// focus, clear, type - the same sequence the search box needs
	if err := b.Eval(ctx, "document.getElementById('box').focus()", nil); err != nil {
		t.Fatal(err)
	}
	if err := b.PressKey(ctx, KeySelectAll); err != nil {
		t.Fatal(err)
	}
	if err := b.PressKey(ctx, KeyDelete); err != nil {
		t.Fatal(err)
	}
	if err := b.TypeText(ctx, "SOC610529"); err != nil {
		t.Fatal(err)
	}

	var value string
	if err := b.Eval(ctx, "document.getElementById('box').value", &value); err != nil {
		t.Fatal(err)
	}
	if value != "SOC610529" {
		t.Fatalf("box holds %q, want %q - clearing or typing did not land", value, "SOC610529")
	}

	// Enter must reach the page as a real key event
	if err := b.PressKey(ctx, KeyEnter); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := b.Eval(ctx, "document.getElementById('out').textContent", &out); err != nil {
		t.Fatal(err)
	}
	if out != "entered:SOC610529" {
		t.Fatalf("out = %q, want the keydown handler to have fired", out)
	}
}

func TestMouseClick(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)
	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}

	var rect struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	js := `(function(){var r=document.getElementById('btn').getBoundingClientRect();
	        return {x:r.left,y:r.top,w:r.width,h:r.height};})()`
	if err := b.Eval(ctx, js, &rect); err != nil {
		t.Fatal(err)
	}
	if rect.W == 0 || rect.H == 0 {
		t.Fatal("button has no size; cannot click it")
	}
	if err := b.ClickAt(ctx, rect.X+rect.W/2, rect.Y+rect.H/2); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := b.Eval(ctx, "document.getElementById('out').textContent", &out); err != nil {
		t.Fatal(err)
	}
	if out != "clicked" {
		t.Fatalf("out = %q, want the click to have registered", out)
	}
}

func TestScreenshot(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)
	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	png, err := b.Screenshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 100 {
		t.Fatalf("screenshot is %d bytes", len(png))
	}
	if string(png[1:4]) != "PNG" {
		t.Fatalf("not a PNG: % x", png[:8])
	}
}

// A dead connection must fail every waiting call rather than hang.
func TestClosedBrowserFailsFast(t *testing.T) {
	b, srv := launch(t)
	ctx := ctxFor(t)
	if err := b.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	b.Close()

	done := make(chan error, 1)
	go func() { done <- b.Eval(ctx, "1+1", nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("evaluating on a closed browser reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("evaluating on a closed browser hung")
	}
}
