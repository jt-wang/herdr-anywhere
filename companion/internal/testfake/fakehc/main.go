// Command fakehc stands in for herdr-core's hc in tests, on every OS: it does
// what the JSON file beside it says (see testfake.Spec), so a test needs no
// shell script, which Windows can't run.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type spec struct {
	Version string `json:"version"`
	Connect string `json:"connect"`
	Exit    int    `json:"exit"`
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
	b, err := os.ReadFile(strings.TrimSuffix(exe, ".exe") + ".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	var s spec
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	switch {
	case arg == "--version" && s.Version != "":
		fmt.Println(s.Version)
	case arg == "connect" && s.Connect == "echo":
		if p := os.Getenv("HM_PIDFILE"); p != "" {
			_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
		}
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case arg == "connect" && strings.HasPrefix(s.Connect, "print:"):
		fmt.Print(strings.TrimPrefix(s.Connect, "print:"))
	}
	os.Exit(s.Exit)
}
