// fakecursor supplies an agent → hook → hc process tree for Cursor's process checks.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"herdr-anywhere/companion/internal/chat/cursor"
)

func main() {
	if filepath.Base(os.Args[0]) == "hc" {
		if os.Getenv("HERDR_CURSOR_TEST_HANG") == "1" {
			time.Sleep(5 * time.Second)
		}
		b, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(os.Getenv("HERDR_CURSOR_TEST_CAPTURE"), b, 0600)
		return
	}
	switch os.Getenv("HERDR_CURSOR_TEST_HELPER") {
	case "agent":
		exe, _ := os.Executable()
		c := exec.Command(exe)
		c.Env = append(os.Environ(), "HERDR_CURSOR_TEST_HELPER=hook")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			os.Exit(3)
		}
	case "hook":
		cursor.Hook(os.Stdin, os.Getenv("HERDR_CURSOR_TEST_HC"))
	case "hold":
		fmt.Println("ready")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
