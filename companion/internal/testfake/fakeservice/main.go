// Command fakeservice stands in for launchctl and systemctl (by the name it is run as) in the self-test of the e2e
// suites' autostart guard (app/e2e/autostart-guard.sh), so a test can load and unload entries without reaching the
// service manager of whoever runs it. What is loaded is a file per label in <this file>.d beside it; every call is a
// line in <this file>.calls.
//
//	launchctl bootstrap DOMAIN PLIST    loads the plist's Label (exit 5 when it is loaded already, as launchctl does)
//	launchctl bootout DOMAIN/LABEL      unloads it (exit 3 when it is not loaded)
//	launchctl list                      "PID Status Label" lines, one per loaded label
//	launchctl print DOMAIN/LABEL        its "path = PLIST" (exit 113 when it is not loaded)
//	systemctl --user enable|start UNIT  loads UNIT without .service; disable [--now] UNIT unloads it
//	systemctl --user list-units ...     one "UNIT loaded active running" line per loaded unit
//
// Anything else exits 0. Run as herdr-anywhere it is a Companion's serve that runs until it is ended.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
	base := strings.TrimSuffix(exe, ".exe")
	dir := base + ".d"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
	args := os.Args[1:]
	if f, err := os.OpenFile(base+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(f, filepath.Base(base), strings.Join(args, " "))
		f.Close()
	}
	if strings.HasPrefix(filepath.Base(base), "herdr-anywhere") {
		for { // a Companion's serve, until it is ended
			time.Sleep(time.Hour)
		}
	}
	if strings.HasPrefix(filepath.Base(base), "systemctl") {
		os.Exit(systemctl(dir, args))
	}
	os.Exit(launchctl(dir, args))
}

var label = regexp.MustCompile(`<key>Label</key>\s*<string>([^<]+)</string>`)

func loaded(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func load(dir, name, from string) int {
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err == nil {
		fmt.Fprintln(os.Stderr, "Bootstrap failed: 5: Input/output error")
		return 5
	}
	if err := os.WriteFile(p, []byte(from+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func unload(dir, name string) int {
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		fmt.Fprintln(os.Stderr, "Boot-out failed: 3: No such process")
		return 3
	}
	return 0
}

func launchctl(dir string, args []string) int {
	if len(args) == 0 {
		return 0
	}
	switch args[0] {
	case "bootstrap":
		if len(args) < 3 {
			return 64
		}
		b, err := os.ReadFile(args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Bootstrap failed: 2: No such file or directory")
			return 2
		}
		m := label.FindSubmatch(b)
		if m == nil {
			return 1
		}
		return load(dir, string(m[1]), args[1]+" "+args[2])
	case "bootout":
		if len(args) < 2 {
			return 64
		}
		return unload(dir, args[1][strings.LastIndex(args[1], "/")+1:])
	case "print":
		if len(args) < 2 {
			return 64
		}
		name := args[1][strings.LastIndex(args[1], "/")+1:]
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not find service %q in domain\n", name)
			return 113
		}
		from := strings.TrimSpace(string(b))
		fmt.Printf("%s = {\n\tpath = %s\n\tstate = not running\n}\n", args[1], from[strings.LastIndex(from, " ")+1:])
	case "list":
		fmt.Println("PID\tStatus\tLabel")
		for _, n := range loaded(dir) {
			fmt.Printf("-\t0\t%s\n", n)
		}
	}
	return 0
}

func systemctl(dir string, args []string) int {
	var words []string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return 0
	}
	unit := func() string { return strings.TrimSuffix(words[len(words)-1], ".service") }
	switch words[0] {
	case "enable", "start":
		if len(words) < 2 {
			return 64
		}
		if _, err := os.Stat(filepath.Join(dir, unit())); err == nil {
			return 0
		}
		return load(dir, unit(), words[0])
	case "disable", "stop":
		if len(words) < 2 {
			return 64
		}
		unload(dir, unit())
	case "list-units":
		for _, n := range loaded(dir) {
			fmt.Printf("%s.service loaded active running\n", n)
		}
	}
	return 0
}
