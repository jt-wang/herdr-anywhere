// A stand-in for Claude Code in the e2e's isolated herdr-core: it reports its status the way Claude Code 2.1.319 does
// (OSC 7501: `state=…:app=claude-code:msg=<base64>`, read from its binary), so hc shows the pane as a
// claude-code agent. FAKE_STATE is reported first; then FAKE_THEN with FAKE_MSG: after FAKE_AFTER seconds, or
// (FAKE_GO_FILE) once that file exists, whichever a suite needs to control the order. Then it waits.
// With no FAKE_STATE it reports nothing and only waits: an agent known by its name, as live-app.tsx's `claude`.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func report(state, msg string) {
	s := "state=" + state + ":app=claude-code"
	if msg != "" {
		s += ":msg=" + base64.StdEncoding.EncodeToString([]byte(msg))
	}
	fmt.Printf("\x1b]7501;%s\x07", s)
}

func main() {
	if state := os.Getenv("FAKE_STATE"); state != "" {
		report(state, "")
	}
	if then := os.Getenv("FAKE_THEN"); then != "" {
		if goFile := os.Getenv("FAKE_GO_FILE"); goFile != "" {
			// A suite that must control the order (a stop is only worth judging when it comes after the
			// hand-off) flips this fake itself, by creating the file, however slow its setup around it is.
			for {
				if _, err := os.Stat(goFile); err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		} else {
			n, _ := strconv.Atoi(os.Getenv("FAKE_AFTER"))
			time.Sleep(time.Duration(n) * time.Second)
		}
		// The credit-window test fills a paused viewer's backlog before reporting done.
		if n, _ := strconv.Atoi(os.Getenv("FAKE_FLOOD_BYTES")); n > 0 {
			for n > 0 {
				size := min(n, 1024)
				fmt.Print(strings.Repeat("x", size))
				n -= size
			}
			fmt.Print("\r\nSYNTHETIC FLOOD COMPLETE\r\n")
		}
		report(then, os.Getenv("FAKE_MSG"))
	}
	time.Sleep(24 * time.Hour)
}
