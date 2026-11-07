package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/update"
)

func buildVersion(t *testing.T, v, out string) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("self-update isn't offered on Windows (update.Supported)")
	}
	cmd := exec.Command("go", "build", "-ldflags=-X main.version="+v, "-o", out, ".")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", v, err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// releaseKey signs the test releases; the program trusts it only when told so with --release-key.
var releaseKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize))

// releaseServer serves a signed release the way github.com/<repo>/releases does, and the flags that point at it.
func releaseServer(t *testing.T, version string, bin []byte) (*httptest.Server, []string) {
	t.Helper()
	sum := sha256.Sum256(bin)
	body, sig, err := update.Sign(releaseKey, update.Manifest{Version: version, Files: []update.File{
		{Name: update.Asset, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(bin))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest/download/"+update.ManifestFile, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/latest/download/"+update.ManifestFile+".sig", func(w http.ResponseWriter, r *http.Request) { w.Write(sig) })
	mux.HandleFunc("/download/v"+version+"/"+update.Asset, func(w http.ResponseWriter, r *http.Request) { w.Write(bin) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	key := base64.StdEncoding.EncodeToString(releaseKey.Public().(ed25519.PublicKey))
	return srv, []string{"--releases", srv.URL, "--release-key", key}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type companionInfo struct {
	Version    string `json:"version"`
	Latest     string `json:"latest"`
	CanUpdate  bool   `json:"canUpdate"`
	AutoUpdate bool   `json:"autoUpdate"`
	Checks     string `json:"checks"`
}

func waitInfo(t *testing.T, url string, ok func(companionInfo) bool) companionInfo {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last companionInfo
	for time.Now().Before(deadline) {
		if r, err := http.Get(url + "/api/info"); err == nil {
			last = companionInfo{}
			json.NewDecoder(r.Body).Decode(&last)
			r.Body.Close()
			if ok(last) {
				return last
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waited for /api/info; last %+v", last)
	return last
}

// The real program: an old file serving, a phone asks, and the same process is then the new version, from the new file.
func TestAPhoneUpdatesARunningCompanion(t *testing.T) {
	dir := t.TempDir()
	bin := buildVersion(t, "0.0.2", filepath.Join(dir, "new"))
	buildVersion(t, "0.0.1", filepath.Join(dir, "herdr-anywhere"))
	exe := filepath.Join(dir, "herdr-anywhere")
	_, flags := releaseServer(t, "0.0.2", bin)

	state := filepath.Join(dir, "state")
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateCode("Phone", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := st.Redeem(code, "test phone")
	if err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	var logs strings.Builder
	cmd := exec.Command(exe, append([]string{"serve", "--listen", fmt.Sprintf("127.0.0.1:%d", port), "--state", state}, flags...)...)
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	// Nothing goes to GitHub until a phone answers yes.
	waitInfo(t, url, func(i companionInfo) bool { return i.Version == "0.0.1" })
	time.Sleep(time.Second)
	if info := waitInfo(t, url, func(companionInfo) bool { return true }); info.Latest != "" || info.Checks != "ask" {
		t.Fatalf("before an answer: %+v", info)
	}
	yes, _ := http.NewRequest(http.MethodPost, url+"/api/update/checks", strings.NewReader(`{"on":true}`))
	yes.Header.Set("Authorization", "Bearer "+token)
	if r, err := http.DefaultClient.Do(yes); err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("answering yes: %v %v", err, r)
	}

	info := waitInfo(t, url, func(i companionInfo) bool { return i.Latest == "0.0.2" })
	if info.Version != "0.0.1" || info.Checks != "on" {
		t.Fatalf("%+v", info)
	}
	if want := update.Supported(); info.CanUpdate != want {
		t.Fatalf("canUpdate = %v", info.CanUpdate)
	}
	if !update.Supported() {
		t.Skip("releases carry no companion for this computer")
	}

	req, _ := http.NewRequest(http.MethodPost, url+"/api/update", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", r.StatusCode, logs.String())
	}

	waitInfo(t, url, func(i companionInfo) bool { return i.Version == "0.0.2" })
	if cmd.ProcessState != nil {
		t.Fatalf("the process ended\n%s", logs.String())
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("not the same process: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(bin) {
		t.Fatal("the file wasn't replaced")
	}
}

func TestUpdateCommand(t *testing.T) {
	if !update.Supported() {
		t.Skip("releases carry no companion for this computer")
	}
	dir := t.TempDir()
	bin := buildVersion(t, "0.0.2", filepath.Join(dir, "new"))
	buildVersion(t, "0.0.1", filepath.Join(dir, "herdr-anywhere"))
	exe := filepath.Join(dir, "herdr-anywhere")
	_, flags := releaseServer(t, "0.0.2", bin)

	out, err := exec.Command(exe, append([]string{"update", "--check"}, flags...)...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "0.0.2 is available") {
		t.Fatalf("%v: %s", err, out)
	}
	if got, _ := os.ReadFile(exe); string(got) == string(bin) {
		t.Fatal("--check replaced the file")
	}
	out, err = exec.Command(exe, append([]string{"update"}, flags...)...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "updated to herdr-anywhere 0.0.2") {
		t.Fatalf("%v: %s", err, out)
	}
	out, err = exec.Command(exe, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "herdr-anywhere 0.0.2" {
		t.Fatalf("%v: %s", err, out)
	}
	out, err = exec.Command(exe, append([]string{"update"}, flags...)...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "is the latest version") {
		t.Fatalf("%v: %s", err, out)
	}
}

// Without being told otherwise the program trusts only the key built into it, so a release signed with another one
// changes nothing.
func TestTheProgramRefusesAReleaseSignedWithAnotherKey(t *testing.T) {
	dir := t.TempDir()
	bin := buildVersion(t, "0.0.2", filepath.Join(dir, "new"))
	buildVersion(t, "0.0.1", filepath.Join(dir, "herdr-anywhere"))
	exe := filepath.Join(dir, "herdr-anywhere")
	srv, _ := releaseServer(t, "0.0.2", bin)
	out, err := exec.Command(exe, "update", "--releases", srv.URL).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "isn't signed by the Herdr Anywhere key") {
		t.Fatalf("%v: %s", err, out)
	}
	if out, _ := exec.Command(exe, "--version").CombinedOutput(); strings.TrimSpace(string(out)) != "herdr-anywhere 0.0.1" {
		t.Fatalf("now %s", out)
	}
}
