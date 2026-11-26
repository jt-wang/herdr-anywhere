package proc

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every program the companion starts goes through Command or CommandContext, so none opens a console window on
// Windows (TestNoConsoleWindowFromTheBackground), except these, which give the program a SysProcAttr of their
// own: serve started detached, and serve started again in the same console (cmd/herdr-anywhere/proc_windows.go).
var ownAttrs = map[string]int{
	"cmd/herdr-anywhere/addphone.go":     1, // spawnServe
	"cmd/herdr-anywhere/proc_windows.go": 1, // execSelf
}

func TestEveryProgramStartsThroughProc(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	call := regexp.MustCompile(`\bexec\.Command(Context)?\(`)
	found := map[string]int{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// Test programs, and this package.
			if rel == "internal/testfake" || rel == "internal/proc" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if l := strings.TrimSpace(line); !strings.HasPrefix(l, "//") && call.MatchString(l) {
				found[rel]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f, n := range found {
		if n != ownAttrs[f] {
			t.Errorf("%s starts %d program(s) with os/exec, want %d: use proc.Command or proc.CommandContext", f, n, ownAttrs[f])
		}
	}
	for f, n := range ownAttrs {
		if found[f] != n {
			t.Errorf("%s: %d os/exec start(s), want %d (update ownAttrs)", f, found[f], n)
		}
	}
}
