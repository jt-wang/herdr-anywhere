package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

// The key tests sign with; the real one signs only real releases.
var (
	testPriv = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	testPub  = testPriv.Public().(ed25519.PublicKey)
)

func testClient(srv *httptest.Server) Client {
	return Client{Releases: srv.URL, Key: testPub, Current: "0.1.5", Asset: testAsset}
}

// fakeGitHub serves a release the way github.com/<repo>/releases does: latest/download/<file> for the newest release,
// download/<tag>/<file> for a given one. digest "" leaves the Mac file out of the release.
func fakeGitHub(t *testing.T, tag string, binary []byte, digest string) (*httptest.Server, *atomic.Int32) {
	return fakeReleases(t, tag, binary, digest, nil)
}

// fakeReleases is fakeGitHub with a chance to change the manifest or its signature after signing; a nil signature is
// missing from the release.
func fakeReleases(t *testing.T, tag string, binary []byte, digest string, tamper func(body, sig []byte) ([]byte, []byte)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	files := []File{{Name: AppAsset, SHA256: "00", Bytes: 136080774}}
	if digest != "" {
		files = append(files, File{Name: testAsset, SHA256: strings.TrimPrefix(digest, "sha256:"), Bytes: int64(len(binary))})
	}
	body, sig, err := Sign(testPriv, Manifest{Version: strings.TrimPrefix(tag, "v"), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if tamper != nil {
		body, sig = tamper(body, sig)
	}
	var checks atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/latest/download/"+ManifestFile, func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "herdr-anywhere/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Write(body)
	})
	mux.HandleFunc("/latest/download/"+ManifestFile+".sig", func(w http.ResponseWriter, r *http.Request) {
		if sig == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(sig)
	})
	mux.HandleFunc("/download/"+tag+"/"+testAsset, func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &checks
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// program is a companion that says it is version: a #!/bin/sh script where one runs, a built program on Windows.
func program(t *testing.T, version string) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		return testfake.Release(t, version, 0)
	}
	return []byte("#!/bin/sh\necho herdr-anywhere " + version + "\n")
}

// failing is a companion whose --version exits with code 3.
func failing(t *testing.T) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		return testfake.Release(t, "", 3)
	}
	return []byte("#!/bin/sh\nexit 3\n")
}

// testAsset is the release file the tests' client installs, whatever computer they run on.
const testAsset = MacAsset

// installed is a companion, version 0.1.5, for a Manager to replace.
func installed(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "herdr-anywhere"+testfake.Ext())
	if err := os.WriteFile(exe, program(t, "0.1.5"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.6", "0.1.5", true},
		{"0.1.10", "0.1.9", true},
		{"0.2.0", "0.1.9", true},
		{"1.0.0", "0.9.9", true},
		{"v0.1.6", "0.1.5", true},
		{"0.1.5", "0.1.5", false},
		{"0.1.4", "0.1.5", false},
		{"", "0.1.5", false},
		{"nonsense", "0.1.5", false},
		{"0.1.6", "dev", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLatestReadsTheMacFile(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	r, err := testClient(srv).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.1.6" || r.URL != srv.URL+"/download/v0.1.6/"+testAsset || r.Digest != digestOf(bin) || r.Bytes != int64(len(bin)) {
		t.Fatalf("%+v", r)
	}
}

// Only a manifest signed by the key built into this program counts: someone able to change the release on GitHub can't
// make computers install a file of theirs.
func TestLatestRefusesAManifestTheKeyDidNotSign(t *testing.T) {
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	for name, tamper := range map[string]func(body, sig []byte) ([]byte, []byte){
		"no signature": func(body, sig []byte) ([]byte, []byte) { return body, nil },
		"another key's signature": func(body, sig []byte) ([]byte, []byte) {
			b, s, _ := Sign(other, Manifest{Version: "0.1.6", Files: []File{{Name: testAsset, SHA256: "ab", Bytes: 1}}})
			return b, s
		},
		"changed after signing": func(body, sig []byte) ([]byte, []byte) {
			return bytes.Replace(body, []byte("0.1.6"), []byte("0.1.9"), 1), sig
		},
		"not a signature": func(body, sig []byte) ([]byte, []byte) { return body, []byte("hello") },
		"empty signature": func(body, sig []byte) ([]byte, []byte) { return body, []byte{} },
	} {
		t.Run(name, func(t *testing.T) {
			bin := program(t, "0.1.6")
			srv, _ := fakeReleases(t, "v0.1.6", bin, digestOf(bin), tamper)
			if r, err := testClient(srv).Latest(context.Background()); err == nil {
				t.Fatalf("accepted %+v", r)
			}
			m := manager(t, srv, installed(t), func() { t.Error("restarted") })
			m.Check(context.Background())
			if m.Latest() != "" || m.App().URL != "" {
				t.Fatalf("remembered %q %+v", m.Latest(), m.App())
			}
		})
	}
}

// The same release checks out under its own key only.
func TestLatestAcceptsWhatTheKeySigned(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	c := testClient(srv)
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("another key accepted it")
	}
}

func TestTheBuiltInKeyIsAKey(t *testing.T) {
	if len(PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("%d bytes", len(PublicKey))
	}
	if c := (Client{}); !bytes.Equal(c.key(), PublicKey) {
		t.Fatal("a client without a key doesn't use the built-in one")
	}
}

func TestLatestRefusesAReleaseWithoutADigest(t *testing.T) {
	srv, _ := fakeGitHub(t, "v0.1.6", program(t, "0.1.6"), "")
	if _, err := (testClient(srv)).Latest(context.Background()); err == nil {
		t.Fatal("no error")
	}
}

func TestApplyReplacesTheFile(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	c := testClient(srv)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exe := installed(t)
	if err := c.Apply(context.Background(), r, exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(bin) {
		t.Fatalf("file holds %q", got)
	}
	if fi, _ := os.Stat(exe); testfake.PermBits() && fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("not executable: %v", fi.Mode())
	}
	assertNoLeftovers(t, exe)
}

// A file that isn't what the release says, or isn't the version it claims, never replaces the one that works.
func TestApplyKeepsTheOldFile(t *testing.T) {
	for name, c := range map[string]struct {
		bin    []byte
		digest string
	}{
		"digest differs": {program(t, "0.1.6"), digestOf([]byte("something else"))},
		"other version":  {program(t, "0.1.4"), digestOf(program(t, "0.1.4"))},
		"doesn't run":    {failing(t), digestOf(failing(t))},
		"not a program":  {[]byte("<html>"), digestOf([]byte("<html>"))},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := fakeGitHub(t, "v0.1.6", c.bin, c.digest)
			cl := testClient(srv)
			r, err := cl.Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			exe := installed(t)
			if err := cl.Apply(context.Background(), r, exe); err == nil {
				t.Fatal("no error")
			}
			got, _ := os.ReadFile(exe)
			if string(got) != string(program(t, "0.1.5")) {
				t.Fatalf("file replaced: %q", got)
			}
			assertNoLeftovers(t, exe)
		})
	}
}

func assertNoLeftovers(t *testing.T, exe string) {
	t.Helper()
	es, _ := os.ReadDir(filepath.Dir(exe))
	if len(es) != 1 {
		t.Fatalf("left in the folder: %v", es)
	}
}

func manager(t *testing.T, srv *httptest.Server, exe string, restart func()) *Manager {
	t.Helper()
	return &Manager{
		Client:   testClient(srv),
		Exe:      exe,
		Installs: true,
		Dir:      t.TempDir(),
		Every:    time.Hour,
		Restart:  restart,
		Logf:     func(string, ...any) {},
	}
}

func TestCheckRemembersANewerVersion(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() { t.Error("restarted") })
	if m.Latest() != "" {
		t.Fatalf("before a check: %q", m.Latest())
	}
	m.Check(context.Background())
	if m.Latest() != "0.1.6" {
		t.Fatalf("Latest = %q", m.Latest())
	}
	if a := m.App(); a.URL != srv.URL+"/download/v0.1.6/"+AppAsset || a.Digest != "sha256:00" || a.Bytes != 136080774 {
		t.Fatalf("App = %+v", a)
	}
}

func TestACheckThatFailsForgetsNothing(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	m.Check(context.Background())
	srv.Close()
	m.Check(context.Background())
	if m.Latest() != "0.1.6" {
		t.Fatalf("Latest = %q", m.Latest())
	}
}

func TestApplyLatestRefusesWithoutANewerVersion(t *testing.T) {
	bin := program(t, "0.1.5")
	srv, _ := fakeGitHub(t, "v0.1.5", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() { t.Error("restarted") })
	if _, err := m.ApplyLatest(context.Background()); err != ErrNoUpdate {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyLatestInstalls(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	exe := installed(t)
	m := manager(t, srv, exe, func() {})
	v, err := m.ApplyLatest(context.Background())
	if err != nil || v != "0.1.6" {
		t.Fatalf("%q %v", v, err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(bin) {
		t.Fatalf("file holds %q", got)
	}
}

func TestAutoStaysOffUntilAskedAndSurvivesARestart(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	if m.Auto() {
		t.Fatal("on by default")
	}
	if err := m.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	again := &Manager{Dir: m.Dir}
	if !again.Auto() {
		t.Fatal("forgotten")
	}
}

func TestACheckUpdatesByItselfOnlyWhenAuto(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	exe := installed(t)
	var restarts atomic.Int32
	m := manager(t, srv, exe, func() { restarts.Add(1) })

	m.Check(context.Background())
	if got, _ := os.ReadFile(exe); string(got) != string(program(t, "0.1.5")) || restarts.Load() != 0 {
		t.Fatalf("updated though auto is off: %q, %d restarts", got, restarts.Load())
	}

	if err := m.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	m.Check(context.Background())
	if got, _ := os.ReadFile(exe); string(got) != string(bin) {
		t.Fatalf("file holds %q", got)
	}
	if restarts.Load() != 1 {
		t.Fatalf("%d restarts", restarts.Load())
	}
}

// A release that fails must not be tried over and over within the hour.
func TestAutoGivesAFailedVersionOneTry(t *testing.T) {
	bin := program(t, "0.1.4") // says 0.1.4 in a release called 0.1.6
	srv, checks := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() { t.Error("restarted") })
	if err := m.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	var logged []string
	m.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	m.Check(context.Background())
	m.Check(context.Background())
	if checks.Load() != 2 {
		t.Fatalf("%d checks", checks.Load())
	}
	failures := 0
	for _, l := range logged {
		if strings.Contains(l, "couldn't") {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("tried %d times: %v", failures, logged)
	}
}

func TestRunChecksAtOnceAndThenOnSchedule(t *testing.T) {
	bin := program(t, "0.1.5")
	srv, checks := fakeGitHub(t, "v0.1.5", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	if err := m.SetChecks(true); err != nil {
		t.Fatal(err)
	}
	m.Every = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for checks.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d checks", checks.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't stop")
	}
}

func TestNothingInstallsWhereNoReleasedFileRuns(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	exe := installed(t)
	m := manager(t, srv, exe, func() { t.Error("restarted") })
	m.Installs = false
	if err := m.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	m.Check(context.Background())
	if _, err := m.ApplyLatest(context.Background()); err != ErrNotHere {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(program(t, "0.1.5")) {
		t.Fatalf("replaced: %q", got)
	}
	if m.Latest() != "0.1.6" {
		t.Fatalf("the notice still needs the version: %q", m.Latest())
	}
}

// Nothing leaves the computer until the person says yes: a program that phones home on its own looks like malware.
func TestRunAsksGitHubNothingUntilThePersonSaysYes(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, checks := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	m.Every = 10 * time.Millisecond
	if m.Checks() != "ask" {
		t.Fatalf("checks = %q before anyone answered", m.Checks())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	if checks.Load() != 0 {
		t.Fatalf("asked GitHub %d times before the person said yes", checks.Load())
	}
	if err := m.SetChecks(true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for checks.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("%d checks after yes", checks.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := m.SetChecks(false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	after := checks.Load()
	time.Sleep(100 * time.Millisecond)
	if checks.Load() != after {
		t.Fatalf("still asking after no: %d then %d", after, checks.Load())
	}
}

// The phone checks the moment the person says yes; the schedule that wakes on the same yes doesn't ask again.
func TestAYesThatTheAppCheckedOnAsksGitHubOnce(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, checks := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	m.First = 20 * time.Millisecond
	m.Every = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	if err := m.SetChecks(true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CheckNow(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if checks.Load() != 1 {
		t.Fatalf("asked GitHub %d times for one yes", checks.Load())
	}
}

func TestTheAnswerSurvivesARestart(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	if err := m.SetChecks(false); err != nil {
		t.Fatal(err)
	}
	if got := (&Manager{Dir: m.Dir}).Checks(); got != "off" {
		t.Fatalf("checks = %q", got)
	}
}

// Installing by itself needs checking; no checks means nothing installs by itself.
func TestAutoAndChecksGoTogether(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	if err := m.SetAuto(true); err != nil {
		t.Fatal(err)
	}
	if m.Checks() != "on" || !m.Auto() {
		t.Fatalf("auto on: checks %q, auto %v", m.Checks(), m.Auto())
	}
	if err := m.SetChecks(false); err != nil {
		t.Fatal(err)
	}
	if m.Checks() != "off" || m.Auto() {
		t.Fatalf("checks off: checks %q, auto %v", m.Checks(), m.Auto())
	}
}

// 0.1.6 wrote only {"auto": true}: whoever turned that on said yes to checking.
func TestA016AutoChoiceCountsAsYes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"auto":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := (&Manager{Dir: dir}).Checks(); got != "on" {
		t.Fatalf("checks = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"auto":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := (&Manager{Dir: dir}).Checks(); got != "ask" {
		t.Fatalf("checks = %q", got)
	}
}

// The person asks now: one question to GitHub, whatever the daily choice, and nothing installs.
func TestCheckNowAsksOnceAndInstallsNothing(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, checks := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	exe := installed(t)
	m := manager(t, srv, exe, func() { t.Error("restarted") })
	if err := m.SetChecks(false); err != nil {
		t.Fatal(err)
	}
	v, err := m.CheckNow(context.Background())
	if err != nil || v != "0.1.6" || checks.Load() != 1 || m.Latest() != "0.1.6" {
		t.Fatalf("CheckNow = %q, %v; %d checks; latest %q", v, err, checks.Load(), m.Latest())
	}
	if got, _ := os.ReadFile(exe); string(got) != string(program(t, "0.1.5")) {
		t.Fatalf("installed: %q", got)
	}
}

// What the last check found is kept: after the computer updates itself, phones still learn of the new app at once.
func TestWhatACheckFoundSurvivesARestart(t *testing.T) {
	bin := program(t, "0.1.6")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	m := manager(t, srv, installed(t), func() {})
	if _, err := m.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	again := &Manager{Dir: m.Dir}
	if again.Latest() != "0.1.6" || again.App().URL == "" {
		t.Fatalf("after a restart: latest %q, app %+v", again.Latest(), again.App())
	}
}

// Each computer installs the one file of a release built for it: the Mac's, or Windows' .exe, which Windows on Arm runs
// too, under its x64 emulation, where the program reports amd64. Linux has no released file.
func TestAssetPerComputer(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "herdr-anywhere-macos-arm64"},
		{"windows", "amd64", "herdr-anywhere-windows-amd64.exe"},
		{"darwin", "amd64", ""},
		{"windows", "arm64", ""},
		{"linux", "amd64", ""},
	} {
		if got := AssetFor(c.goos, c.goarch); got != c.want {
			t.Errorf("AssetFor(%s, %s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
	if Supported() != (AssetFor(runtime.GOOS, runtime.GOARCH) != "") {
		t.Errorf("Supported() = %v on %s/%s", Supported(), runtime.GOOS, runtime.GOARCH)
	}
}

// A release with the Mac's and Windows' files: a Windows computer takes the .exe, with its own digest and size.
func TestLatestReadsTheFileForThisComputer(t *testing.T) {
	mac, win := []byte("mac file"), []byte("windows file!")
	body, sig, err := Sign(testPriv, Manifest{Version: "0.1.6", Files: []File{
		{Name: MacAsset, SHA256: strings.TrimPrefix(digestOf(mac), "sha256:"), Bytes: int64(len(mac))},
		{Name: WindowsAsset, SHA256: strings.TrimPrefix(digestOf(win), "sha256:"), Bytes: int64(len(win))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest/download/"+ManifestFile, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/latest/download/"+ManifestFile+".sig", func(w http.ResponseWriter, r *http.Request) { w.Write(sig) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := testClient(srv)
	c.Asset = WindowsAsset
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != srv.URL+"/download/v0.1.6/"+WindowsAsset || r.Digest != digestOf(win) || r.Bytes != int64(len(win)) {
		t.Fatalf("%+v", r)
	}
}

// The companion replaces its own file while it runs: Windows doesn't let a running program's file be written over or
// removed, only renamed, so the old one goes aside (exe + ".old") and goes away at a later start.
func TestApplyReplacesARunningProgram(t *testing.T) {
	bin := testfake.Release(t, "0.1.6", 0)
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	c := testClient(srv)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "herdr-anywhere"+testfake.Ext())
	if err := os.WriteFile(exe, testfake.Release(t, "0.1.5", 0), 0o755); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(exe, "wait")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { running.Process.Kill(); running.Wait() }()

	if err := c.Apply(context.Background(), r, exe); err != nil {
		t.Fatalf("replacing a running program: %v", err)
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, bin) {
		t.Fatal("the file wasn't replaced")
	}
	out, err := exec.Command(exe, "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "herdr-anywhere 0.1.6" {
		t.Fatalf("the new file: %v %q", err, out)
	}

	running.Process.Kill()
	running.Wait()
	RemoveOld(exe)
	if es, _ := os.ReadDir(filepath.Dir(exe)); len(es) != 1 {
		t.Fatalf("left in the folder after the old one ended: %v", es)
	}
}

// A computer releases carry no file for (Linux, Windows on Arm built for Arm) still hears of a new version, so the
// phone can say so and show how to install it by hand; Apply refuses it.
func TestLatestOnAComputerWithoutAReleaseFile(t *testing.T) {
	old := Asset
	Asset = AssetFor("linux", "amd64")
	t.Cleanup(func() { Asset = old })
	bin := []byte("mac file")
	srv, _ := fakeGitHub(t, "v0.1.6", bin, digestOf(bin))
	c := testClient(srv)
	c.Asset = ""
	r, err := c.Latest(context.Background())
	if err != nil || r.Version != "0.1.6" || r.URL != "" {
		t.Fatalf("%+v, %v", r, err)
	}
	if err := c.Apply(context.Background(), r, filepath.Join(t.TempDir(), "herdr-anywhere")); !errors.Is(err, ErrNotHere) {
		t.Fatalf("Apply: %v", err)
	}
}
