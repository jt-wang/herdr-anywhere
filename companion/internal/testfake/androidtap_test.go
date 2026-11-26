package testfake

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// app/e2e/android-lib.sh's tap, as e2e/android.sh runs it (sh, set -eu), against a fake adb. On CI an Android run
// ended with exit 137 and no FAIL line: a screen dump killed on the phone, and set -e ended the run there.
func TestAndroidTap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("android.sh runs in sh; its CI job is on Linux")
	}
	lib, err := filepath.Abs("../../../app/e2e/android-lib.sh")
	if err != nil {
		t.Fatal(err)
	}
	pair := `<node resource-id="pair" bounds="[10,20][30,40]"/>`
	other := `<node resource-id="pair-link" bounds="[0,0][2,2]"/>`
	for _, c := range []struct {
		name string
		spec ADBSpec
	}{
		{"a dump killed once", ADBSpec{Dumps: []int{137}, Screens: []string{pair}}},
		{"the view on the second look", ADBSpec{Screens: []string{other, other, pair}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.spec.Calls = filepath.Join(dir, "calls")
			adb := ADB(t, dir, c.spec)
			cmd := exec.Command("sh", "-c", `set -eu; . "$1"; tap pair; echo tapped`, "sh", lib)
			cmd.Env = append(os.Environ(), "adb="+adb)
			out, err := cmd.CombinedOutput()
			calls, _ := os.ReadFile(c.spec.Calls)
			if err != nil || !strings.Contains(string(out), "tapped") {
				t.Fatalf("tap pair: %v, said %q; adb calls:\n%s", err, out, calls)
			}
			if !strings.Contains(string(calls), "shell input tap 20 30\n") {
				t.Fatalf("no tap at pair's centre; adb calls:\n%s", calls)
			}
		})
	}
}

// app/e2e/android-lib.sh's tap_steady: a view that just moved (on CI, a sheet laid out again after the screen's size was
// put back) is tapped where two fresh dumps in a row found it, and only there.
func TestAndroidTapSteady(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("android.sh runs in sh; its CI job is on Linux")
	}
	lib, err := filepath.Abs("../../../app/e2e/android-lib.sh")
	if err != nil {
		t.Fatal(err)
	}
	at := func(x int) string {
		return fmt.Sprintf(`<node resource-id="new-terminal" bounds="[%d,10][%d,30]"/>`, x-10, x+10)
	}
	var moving []string
	for i := 0; i < 11; i++ {
		moving = append(moving, at(100+80*i))
	}
	for _, c := range []struct {
		name  string
		spec  ADBSpec
		tap   string // the one tap expected, or none
		dumps int    // dumps before it
	}{
		// Looked up again after the two that agreed, it would be tapped at 900,20, which no dump checked.
		{"tapped where it stayed, not looked up again", ADBSpec{Screens: []string{at(100), at(100), at(900)}}, "100 20", 2},
		// A failed dump leaves the last file: read again, it would agree with the first.
		{"a failed dump starts the count over", ADBSpec{Dumps: []int{0, 137}, Screens: []string{at(100)}}, "100 20", 4},
		{"never at one place: no tap", ADBSpec{Screens: moving}, "", 10},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.spec.Calls = filepath.Join(dir, "calls")
			adb := ADB(t, dir, c.spec)
			cmd := exec.Command("sh", "-c", `set -eu; . "$1"; tap_steady new-terminal; echo tapped`, "sh", lib)
			cmd.Env = append(os.Environ(), "adb="+adb)
			out, err := cmd.CombinedOutput()
			b, _ := os.ReadFile(c.spec.Calls)
			calls := string(b)
			var taps []string
			dumps, before := 0, -1
			for _, l := range strings.Split(calls, "\n") {
				if strings.HasPrefix(l, "shell uiautomator dump") {
					dumps++
				}
				if strings.HasPrefix(l, "shell input tap ") {
					taps = append(taps, strings.TrimPrefix(l, "shell input tap "))
					before = dumps
				}
			}
			if c.tap == "" {
				if err == nil || len(taps) != 0 || !strings.Contains(string(out), "FAIL: new-terminal was not at one place") || dumps != c.dumps {
					t.Fatalf("want a FAIL, no tap and %d dumps; got %v, %q; adb calls:\n%s", c.dumps, err, out, calls)
				}
				return
			}
			if err != nil || len(taps) != 1 || taps[0] != c.tap || before != c.dumps || !strings.Contains(string(out), "tapped at "+strings.ReplaceAll(c.tap, " ", ",")) {
				t.Fatalf("want one tap at %s after %d dumps; got %v, %q; adb calls:\n%s", c.tap, c.dumps, err, out, calls)
			}
		})
	}
}
