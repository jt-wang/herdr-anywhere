// chatevidence provides simulator container operations for the cleanup test.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if filepath.Base(os.Args[0]) == "suite" {
		err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"mode": os.Getenv("CHAT_AWAY_ONLY"), "agents": os.Getenv("AGENTS"),
			"cycles": os.Getenv("CHAT_AWAY_CYCLES"), "control": os.Getenv("CHAT_AWAY_SELFTEST"),
			"args": os.Args[1:],
		})
		if err != nil {
			panic(err)
		}
		return
	}

	if filepath.Base(os.Args[0]) == "python3" {
		if len(os.Args) == 4 && filepath.Base(os.Args[1]) == "ios-background-expiry.py" && os.Args[2] == "decode" {
			code, err := strconv.Atoi(os.Getenv("EVIDENCE_TEST_DECODE_EXIT"))
			if err != nil {
				panic(err)
			}
			record := `[{"exit":0,"timeout":false,"decoded":true}]`
			if code != 0 {
				record = `[{"exit":-15,"timeout":true,"decoded":false}]`
			}
			if err := os.WriteFile(filepath.Join(os.Args[3], "decode.json"), []byte(record), 0600); err != nil {
				panic(err)
			}
			os.Exit(code)
		}
		cmd := exec.Command(os.Getenv("EVIDENCE_TEST_PYTHON"), os.Args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				os.Exit(e.ExitCode())
			}
			panic(err)
		}
		return
	}

	if filepath.Base(os.Args[0]) != "xcrun" {
		return
	}
	if log := os.Getenv("EVIDENCE_TEST_CALLS"); log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
		if err := f.Close(); err != nil {
			panic(err)
		}
	}
	if len(os.Args) < 3 {
		os.Exit(2)
	}
	switch os.Args[2] {
	case "get_app_container":
		fmt.Println(os.Getenv("EVIDENCE_TEST_CONTAINER"))
	case "uninstall":
		if err := os.RemoveAll(os.Getenv("EVIDENCE_TEST_CONTAINER")); err != nil {
			panic(err)
		}
	}
}
