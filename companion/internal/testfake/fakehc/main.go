// Command fakehc stands in for herdr-core's hc in tests, on every OS: it does
// what the JSON file beside it says (see testfake.Spec), so a test needs no
// shell script, which Windows can't run.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// spec and reply are testfake.Spec and testfake.Reply.
type spec struct {
	Version string             `json:"version"`
	Connect string             `json:"connect"`
	Exit    int                `json:"exit"`
	Calls   string             `json:"calls"`
	On      map[string][]reply `json:"on"`
	Else    int                `json:"else"`
}

type reply struct {
	If, Write, Append, Print, Cat, Exec, Fail, Sleep string
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
	line := strings.Join(os.Args[1:], " ") + "\n"
	if s.Calls != "" {
		add(s.Calls, line)
	}
	if replies, ok := s.On[arg]; ok && arg != "--version" {
		second := ""
		if len(os.Args) > 2 {
			second = os.Args[2]
		}
		fill := func(x string) string { return strings.ReplaceAll(x, "{2}", second) }
		for _, r := range replies {
			if r.If != "" {
				if _, err := os.Stat(fill(r.If)); err != nil {
					continue
				}
			}
			if d, err := time.ParseDuration(r.Sleep); err == nil {
				time.Sleep(d)
			}
			if r.Write != "" {
				_ = os.WriteFile(fill(r.Write), []byte(line), 0o644)
			}
			if r.Append != "" {
				add(fill(r.Append), line)
			}
			if r.Print != "" {
				fmt.Println(fill(r.Print))
			}
			if r.Fail != "" {
				b, _ := os.ReadFile(fill(r.Fail))
				os.Stderr.Write(b)
				os.Exit(5)
			}
			if r.Exec != "" {
				execOn(r.Exec, os.Args[2:])
			}
			if r.Cat != "" {
				b, err := os.ReadFile(fill(r.Cat))
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				os.Stdout.Write(b)
			}
			break
		}
		os.Exit(s.Exit)
	}
	if s.On != nil && arg != "--version" {
		os.Exit(s.Else)
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

// execOn is `hc exec --on M --json …`: the program runs here, as if on machine M, whose home is root/M (a machine
// without one is unknown), and its result is printed as hc prints one (machine.exec's ExecResult): what it wrote as
// base64, stdout cut at 8 MB.
func execOn(root string, args []string) {
	on := ""
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--on", "--timeout", "--cwd", "--env":
			if args[0] == "--on" && len(args) > 1 {
				on = args[1]
			}
			args = args[2:]
		default:
			args = args[1:]
		}
	}
	home := filepath.Join(root, on)
	if st, err := os.Stat(home); on == "" || err != nil || !st.IsDir() || len(args) == 0 {
		fmt.Fprintln(os.Stderr, `{"error":{"kind":"machine_unknown","message":"unknown machine `+on+`"}}`)
		os.Exit(4)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "TMPDIR=")
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	start := time.Now()
	err := cmd.Run()
	code := 0
	var x *exec.ExitError
	if errors.As(err, &x) {
		code = x.ExitCode()
	} else if err != nil {
		fmt.Fprintln(os.Stderr, `{"error":{"kind":"spawn_failed","message":"no such program"}}`)
		os.Exit(5)
	}
	const max = 8 << 20
	b := out.Bytes()
	cut := len(b) > max
	if cut {
		b = b[:max]
	}
	j, _ := json.Marshal(map[string]any{"code": code, "ms": time.Since(start).Milliseconds(), "signal": nil,
		"stdout": base64.StdEncoding.EncodeToString(b), "stderr": base64.StdEncoding.EncodeToString(errs.Bytes()),
		"truncated": cut, "timed_out": false})
	fmt.Println(string(j))
}

func add(file, line string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(96)
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}
