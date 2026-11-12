package testfake

import (
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
