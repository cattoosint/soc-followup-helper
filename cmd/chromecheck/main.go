// Command chromecheck answers one question: can this machine's Chrome be
// driven at all? Run it before blaming the tool.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cattoosint/socfollowup-test/internal/cdp"
)

func main() {
	path, err := cdp.FindChrome()
	if err != nil {
		fmt.Println("FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("browser:", path)

	profile, err := os.MkdirTemp("", "chromecheck_")
	if err != nil {
		fmt.Println("FAILED:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(profile)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	b, err := cdp.Launch(ctx, cdp.Options{
		ChromePath: path,
		ProfileDir: profile,
		Headless:   os.Getenv("CHROMECHECK_VISIBLE") == "",
	})
	if err != nil {
		fmt.Println("FAILED to start or attach:", err)
		os.Exit(1)
	}
	defer b.Close()

	if err := b.Navigate(ctx, "about:blank"); err != nil {
		fmt.Println("FAILED to navigate:", err)
		os.Exit(1)
	}

	var agent string
	if err := b.Eval(ctx, "navigator.userAgent", &agent); err != nil {
		fmt.Println("FAILED to run script:", err)
		os.Exit(1)
	}

	fmt.Println("OK: driving", agent)
}
