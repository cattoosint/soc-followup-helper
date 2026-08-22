package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Key is one keyboard key, described the way the protocol wants it.
type Key struct {
	Key       string
	Code      string
	VirtKey   int
	Text      string
	Modifiers int
}

// The keys this tool presses. Modifiers: 1 alt, 2 ctrl, 4 meta, 8 shift.
var (
	KeyEnter     = Key{Key: "Enter", Code: "Enter", VirtKey: 13, Text: "\r"}
	KeyEscape    = Key{Key: "Escape", Code: "Escape", VirtKey: 27}
	KeyDelete    = Key{Key: "Delete", Code: "Delete", VirtKey: 46}
	KeySelectAll = Key{Key: "a", Code: "KeyA", VirtKey: 65, Modifiers: 2}
)

type evalResult struct {
	Result struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text      string `json:"text"`
		Exception struct {
			Description string `json:"description"`
		} `json:"exception"`
	} `json:"exceptionDetails"`
}

// Eval runs JavaScript in the page and decodes its value into out, which may
// be nil when the result is not wanted.
//
// The whole browser layer above this is built on Eval: elements are found,
// read and stamped by scripts rather than by protocol-level DOM calls, which
// keeps that code close to the Python original it was ported from.
func (b *Browser) Eval(ctx context.Context, expression string, out any) error {
	var res evalResult
	err := b.Send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}, &res)
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		msg := res.ExceptionDetails.Exception.Description
		if msg == "" {
			msg = res.ExceptionDetails.Text
		}
		return fmt.Errorf("javascript failed: %s", msg)
	}
	if out == nil || len(res.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(res.Result.Value, out)
}

// Navigate opens a URL and waits for the document to finish loading.
func (b *Browser) Navigate(ctx context.Context, url string) error {
	if err := b.Send(ctx, "Page.navigate", map[string]any{"url": url}, nil); err != nil {
		return err
	}
	return b.WaitReady(ctx)
}

// WaitReady blocks until the document is complete, or the context gives up.
func (b *Browser) WaitReady(ctx context.Context) error {
	for {
		var state string
		if err := b.Eval(ctx, "document.readyState", &state); err == nil {
			if state == "complete" || state == "interactive" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// CurrentURL reports where the page is.
func (b *Browser) CurrentURL(ctx context.Context) (string, error) {
	var url string
	err := b.Eval(ctx, "location.href", &url)
	return url, err
}

// PressKey sends one key down and up.
func (b *Browser) PressKey(ctx context.Context, k Key) error {
	down := map[string]any{
		"type":                  "keyDown",
		"key":                   k.Key,
		"code":                  k.Code,
		"windowsVirtualKeyCode": k.VirtKey,
		"nativeVirtualKeyCode":  k.VirtKey,
		"modifiers":             k.Modifiers,
	}
	if k.Text != "" {
		down["text"] = k.Text
	}
	if err := b.Send(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return b.Send(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type":                  "keyUp",
		"key":                   k.Key,
		"code":                  k.Code,
		"windowsVirtualKeyCode": k.VirtKey,
		"nativeVirtualKeyCode":  k.VirtKey,
		"modifiers":             k.Modifiers,
	}, nil)
}

// TypeText types a string one character at a time.
//
// Real key events rather than Input.insertText: Outlook's search box is driven
// by a framework that listens for keystrokes, and a bulk insert does not
// always reach it.
func (b *Browser) TypeText(ctx context.Context, text string) error {
	for _, r := range text {
		s := string(r)
		vk := 0
		switch {
		case r >= 'a' && r <= 'z':
			vk = int(r - 32) // virtual keys are the uppercase code
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			vk = int(r)
		}
		if err := b.Send(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type":                  "keyDown",
			"key":                   s,
			"text":                  s,
			"unmodifiedText":        s,
			"windowsVirtualKeyCode": vk,
			"nativeVirtualKeyCode":  vk,
		}, nil); err != nil {
			return err
		}
		if err := b.Send(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type":                  "keyUp",
			"key":                   s,
			"windowsVirtualKeyCode": vk,
			"nativeVirtualKeyCode":  vk,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ClickAt presses the left mouse button at viewport coordinates.
func (b *Browser) ClickAt(ctx context.Context, x, y float64) error {
	for _, kind := range []string{"mousePressed", "mouseReleased"} {
		if err := b.Send(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type":       kind,
			"x":          x,
			"y":          y,
			"button":     "left",
			"clickCount": 1,
			"buttons":    1,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// Screenshot captures the visible page as PNG bytes.
func (b *Browser) Screenshot(ctx context.Context) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := b.Send(ctx, "Page.captureScreenshot",
		map[string]any{"format": "png"}, &res); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Data)
}

// SetViewport fixes the page size. Used by tooling that renders the tracker;
// a real run leaves the window as the analyst sized it.
func (b *Browser) SetViewport(ctx context.Context, width, height int) error {
	return b.Send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}, nil)
}
