//go:build !windows

package main

// TestLiveLoginEntry runs serve under this computer's real service manager, from the entry internal/autostart writes,
// under a name of its own and with a plist or unit in a temporary home: launchd on macOS (HM_LAUNCHD_LIVE=1), the
// systemd user instance on Linux (HM_SYSTEMD_LIVE=1, CI). It checks that the manager starts serve, starts it again
// after a crash, still runs the right file after `update` replaced it, and that Remove stops it for good. A login
// itself isn't part of it: launchd loads ~/Library/LaunchAgents and systemd starts default.target's units at login,
// which Start does here by hand.

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/autostart"
	"herdr-anywhere/companion/internal/testfake"
	"herdr-anywhere/companion/internal/update"
)

func TestLiveLoginEntry(t *testing.T) {
	switch {
	case runtime.GOOS == "darwin" && os.Getenv("HM_LAUNCHD_LIVE") == "1":
	case runtime.GOOS == "linux" && os.Getenv("HM_SYSTEMD_LIVE") == "1":
		if out, err := exec.Command("systemctl", "--user", "show-environment").CombinedOutput(); err != nil {
			t.Fatalf("HM_SYSTEMD_LIVE=1, but there is no systemd user instance: %v: %s", err, out)
		}
	default:
		t.Skip("HM_LAUNCHD_LIVE=1 (macOS) or HM_SYSTEMD_LIVE=1 (Linux) runs serve under the real service manager")
	}
	if !update.Supported() {
		t.Skip("releases carry no companion for this computer")
	}
	bin := t.TempDir()
	newer := buildVersion(t, "0.0.2", filepath.Join(bin, "new"))
	buildVersion(t, "0.0.1", filepath.Join(bin, "herdr-anywhere"))
	exe := filepath.Join(bin, "herdr-anywhere"+testfake.Ext())
	hc := testfake.HC(t, bin, "hc", testfake.Spec{Version: "hc 0.1.19"})
	dir := shortStateDir(t)

	b := make([]byte, 4)
	rand.Read(b)
	s := &autostart.Service{Name: "io.jingtao.herdranywhere.test-" + hex.EncodeToString(b), Home: t.TempDir()}
	t.Cleanup(func() { s.Remove() })
	log := filepath.Join(dir, "serve.log")
	e := autostart.Entry{Exe: exe, Args: []string{"serve", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir},
		Env: map[string]string{"PATH": os.Getenv("PATH")}, Log: log}
	if _, err := s.Install(e); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	logs := fileLog(log)

	// running reports serve's pid and version, once one answers that isn't `not`.
	running := func(not int) (int, string) {
		t.Helper()
		var pid int
		var info companionInfo
		waitFor(t, 30*time.Second, func() bool {
			urls := readServeURLs(dir)
			pid = recordedPID(dir)
			if urls == nil || pid == 0 || pid == not {
				return false
			}
			info = waitInfo(t, urls[0], func(companionInfo) bool { return true })
			return true
		}, "serve under the manager", logs)
		return pid, info.Version
	}
	pid, v := running(0)
	t.Logf("started by the manager: pid %d, %s", pid, v)

	syscall.Kill(pid, syscall.SIGKILL)
	pid2, v := running(pid)
	t.Logf("after a crash: pid %d, %s", pid2, v)

	_, flags := releaseServer(t, "0.0.2", newer)
	if out, err := exec.Command(exe, append([]string{"update"}, flags...)...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	syscall.Kill(pid2, syscall.SIGKILL)
	pid3, v := running(pid2)
	if v != "0.0.2" {
		t.Fatalf("after the update the manager runs %s", v)
	}
	t.Logf("after an update and a crash: pid %d, %s", pid3, v)

	if removed, err := s.Remove(); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	waitFor(t, 10*time.Second, func() bool { return !alive(pid3) }, "serve to end once removed", logs)
	time.Sleep(3 * time.Second)
	if p := recordedPID(dir); p != pid3 && readServeURLs(dir) != nil {
		t.Fatalf("serve came back as %d after Remove", p)
	}
}
