// nshcli stands in for Git and Rust when testing the CLI build shell.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const base = "76fa8a7fed47ea51aea01fa85bb40960be15d984"

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	root := os.Getenv("NSH_CLI_TEST_ROOT")
	f, err := os.OpenFile(filepath.Join(root, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	must(err)
	fmt.Fprintln(f, filepath.Base(os.Args[0])+" "+strings.Join(os.Args[1:], " "))
	must(f.Close())
	a := os.Args[1:]
	switch filepath.Base(os.Args[0]) {
	case "git":
		if len(a) >= 2 && a[0] == "-C" {
			a = a[2:]
		}
		if len(a) == 0 {
			os.Exit(2)
		}
		switch a[0] {
		case "clone":
			must(os.MkdirAll(filepath.Join(a[len(a)-1], ".git"), 0700))
		case "rev-parse":
			if os.Getenv("NSH_CLI_TEST_FAIL") == "base" {
				fmt.Println(strings.Repeat("0", 40))
			} else {
				fmt.Println(base)
			}
		case "status":
			if os.Getenv("NSH_CLI_TEST_FAIL") == "dirty" {
				fmt.Println(" M Cargo.toml")
			}
		case "apply":
			p := a[len(a)-1]
			if _, err := os.Stat(p); err != nil {
				os.Exit(3)
			}
		default:
			fmt.Fprintln(os.Stderr, "unexpected git operation")
			os.Exit(2)
		}
	case "rustup":
		if strings.Join(a, " ") != "run 1.99.1 cargo build --release -p nsh-cli --locked" {
			os.Exit(4)
		}
		if os.Getenv("NSH_CLI_TEST_FAIL") == "build" {
			fmt.Fprintln(os.Stderr, "synthetic build failure")
			os.Exit(17)
		}
		if os.Getenv("CC_SHELL_ESCAPED_FLAGS") != "1" {
			fmt.Fprintln(os.Stderr, "missing cc shell-escaped flags mode")
			os.Exit(6)
		}
		src, err := os.Getwd()
		must(err)
		paths := []string{src, os.Getenv("CARGO_HOME"), os.Getenv("CARGO_TARGET_DIR")}
		aliases := []string{"/nsh", "/cargo", "/target"}
		flags := strings.Split(os.Getenv("CARGO_ENCODED_RUSTFLAGS"), "\x1f")
		for i, p := range paths {
			want := "--remap-path-prefix=" + p + "=" + aliases[i]
			found := false
			for _, flag := range flags {
				if flag == want {
					found = true
				}
			}
			if !found || !strings.Contains(os.Getenv("CFLAGS"), "\"-ffile-prefix-map="+p+"="+aliases[i]+"\"") {
				fmt.Fprintln(os.Stderr, "missing build path remap")
				os.Exit(5)
			}
		}
		out := filepath.Join(os.Getenv("CARGO_TARGET_DIR"), "release", "nsh")
		must(os.MkdirAll(filepath.Dir(out), 0700))
		must(os.WriteFile(out, []byte("synthetic patched CLI\n"), 0700))
	default:
		os.Exit(2)
	}
}
