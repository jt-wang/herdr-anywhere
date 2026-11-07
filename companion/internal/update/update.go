// Package update finds out whether a newer companion was released, and replaces this one with it.
//
// The computer asks GitHub, so the phone talks to nothing but the computer until the person asks for a new app.
package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Repo holds the releases.
	Repo = "jt-wang/herdr-anywhere"
	// MacAsset is a release's companion for Apple silicon Macs.
	MacAsset = "herdr-anywhere-macos-arm64"
	// WindowsAsset is a release's companion for Windows: x64, which Windows on Arm runs under its emulation.
	WindowsAsset = "herdr-anywhere-windows-amd64.exe"
	// AppAsset is the Android app in a release.
	AppAsset = "Herdr-Anywhere.apk"

	// github.com/<repo>/releases/latest/download/<file> is the newest release's file: a redirect to download/v<version>/<file>,
	// 404 when that release has no such file (measured 2026-11-06 on v0.1.7). No GitHub API, so no hourly limit.
	defaultReleases = "https://github.com/" + Repo + "/releases"
	maxManifest     = 64 << 10
	// Far above the file (18 MB in 0.1.5), to stop a wrong answer filling the disk.
	maxBinary = 256 << 20
	stateFile = "update.json"
)

// ErrNoUpdate says there is nothing newer to install.
var ErrNoUpdate = errors.New("already the latest version")

// ErrNotHere says no released file runs on this computer.
var ErrNotHere = errors.New("releases don't include a companion for this computer")

// ErrBusy says an update is already being installed.
var ErrBusy = errors.New("already updating")

// AssetFor is the release's companion file for a computer, "" for one releases carry none for. A program built for
// amd64 says amd64 on Windows on Arm too, where it runs under emulation.
func AssetFor(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return MacAsset
	case "windows/amd64":
		return WindowsAsset
	}
	return ""
}

// Asset is this computer's file in a release.
var Asset = AssetFor(runtime.GOOS, runtime.GOARCH)

// Supported says a released file can run here.
func Supported() bool { return Asset != "" }

// Newer says version a comes after b. Anything that isn't numbers, such as a developer's build, is never newer or older.
func Newer(a, b string) bool {
	x, ok := parts(a)
	y, ok2 := parts(b)
	if !ok || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func parts(v string) ([3]int, bool) {
	var out [3]int
	f := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(f) != 3 {
		return out, false
	}
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Release is the newest published companion.
type Release struct {
	Version string
	URL     string
	Digest  string // "sha256:<hex>"
	Bytes   int64
	App     App
}

// App is the Android app in a release. The phone downloads it; the computer only says where it is, so the phone talks
// to nothing else until the person asks.
type App struct {
	URL    string `json:"url"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

// Client reads releases. Releases empty means this repo's on GitHub; Key nil means PublicKey.
type Client struct {
	Releases string
	Key      ed25519.PublicKey
	Current  string
	HTTP     *http.Client
	// Asset is the release file to install; empty is this computer's (Asset).
	Asset string
}

func (c Client) asset() string {
	if c.Asset != "" {
		return c.Asset
	}
	return Asset
}

func (c Client) key() ed25519.PublicKey {
	if c.Key != nil {
		return c.Key
	}
	return PublicKey
}

func (c Client) releases() string {
	if c.Releases != "" {
		return strings.TrimSuffix(c.Releases, "/")
	}
	return defaultReleases
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "herdr-anywhere/"+c.Current)
	r, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		return nil, fmt.Errorf("%s answered %s", req.URL.Host, r.Status)
	}
	return r, nil
}

// Latest reads the newest release that isn't a draft or a pre-release, and only if the key signed it.
func (c Client) Latest(ctx context.Context) (Release, error) {
	base := c.releases()
	body, err := c.read(ctx, base+"/latest/download/"+ManifestFile, maxManifest)
	if err != nil {
		return Release{}, err
	}
	sig, err := c.read(ctx, base+"/latest/download/"+ManifestFile+".sig", 1<<10)
	if err != nil {
		return Release{}, fmt.Errorf("the release has no signature: %w", err)
	}
	m, err := Verify(c.key(), body, sig)
	if err != nil {
		return Release{}, err
	}
	rel := Release{Version: m.Version}
	at := func(name string) string { return base + "/download/v" + m.Version + "/" + name }
	for _, f := range m.Files {
		if _, err := hex.DecodeString(f.SHA256); err != nil || f.SHA256 == "" {
			continue
		}
		switch {
		case c.asset() != "" && f.Name == c.asset():
			rel.URL, rel.Digest, rel.Bytes = at(f.Name), "sha256:"+f.SHA256, f.Bytes
		case f.Name == AppAsset:
			rel.App = App{URL: at(f.Name), Digest: "sha256:" + f.SHA256, Bytes: f.Bytes}
		}
	}
	// A computer releases carry no file for still hears of the version; Apply refuses it.
	if rel.URL == "" && c.asset() != "" {
		return Release{}, fmt.Errorf("release %s has no %s", m.Version, c.asset())
	}
	return rel, nil
}

func (c Client) read(ctx context.Context, url string, limit int64) ([]byte, error) {
	r, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is too large", url)
	}
	return b, nil
}

// Apply downloads the release beside exe and puts it in exe's place. It does so only once the file matches the release's
// digest, and runs, and says it is that version; until then exe stays as it was.
func (c Client) Apply(ctx context.Context, rel Release, exe string) error {
	if rel.URL == "" {
		return ErrNotHere
	}
	// The same ending as exe: Windows runs only a file named .exe.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".herdr-anywhere-update-*"+filepath.Ext(exe))
	if err != nil {
		return err
	}
	defer removeSoon(tmp.Name()) // after it ran, Windows on Arm holds an x64 program's file a moment longer
	defer tmp.Close()

	r, err := c.get(ctx, rel.URL)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(r.Body, maxBinary+1))
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	if n > maxBinary || (rel.Bytes > 0 && n != rel.Bytes) {
		return errors.New("the download isn't the size the release says")
	}
	if got := "sha256:" + hex.EncodeToString(hash.Sum(nil)); got != rel.Digest {
		return errors.New("the download doesn't match the release's digest")
	}
	if runtime.GOOS != "windows" { // no permission bits there: any file named .exe runs
		if err := tmp.Chmod(0o755); err != nil {
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	try, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(try, tmp.Name(), "--version")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the new file doesn't run: %w", err)
	}
	if got, want := strings.TrimSpace(out.String()), "herdr-anywhere "+rel.Version; got != want {
		return fmt.Errorf("the new file says %q, not %q", got, want)
	}
	return replace(tmp.Name(), exe)
}

// RemoveOld removes the files an update set aside on Windows (exe + ".old", exe + ".old-<n>"), trying for a while:
// right after a restart the old process may still hold one. Elsewhere there are none.
func RemoveOld(exe string) {
	olds, _ := filepath.Glob(exe + ".old*")
	removeSoon(olds...)
}

// removeSoon removes files, trying again for 10 s while Windows still holds one.
func removeSoon(paths ...string) {
	for i := 0; i < 20 && len(paths) > 0; i++ {
		left := paths[:0]
		for _, p := range paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				left = append(left, p)
			}
		}
		if paths = left; len(paths) > 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// Manager keeps a running companion up to date.
type Manager struct {
	Client Client
	// Exe is the file to replace.
	Exe string
	// Dir holds the person's choices and what the last check found.
	Dir string
	// Installs says a released file can run here (Supported, outside tests).
	Installs bool
	// First is how long after starting to ask for the first time, once the person said yes.
	First time.Duration
	// Every is how often to ask, once the person said yes.
	Every time.Duration
	// Restart runs the new file. It's called once the old one is replaced by an automatic update; after a person's
	// request the caller calls it, having answered first.
	Restart func()
	Logf    func(string, ...any)

	mu       sync.Mutex
	failed   string    // the version an automatic update already failed on
	looked   time.Time // when GitHub last answered, on schedule or asked from a phone
	applying bool
	wake     chan struct{}
}

// saved is update.json: the person's answers, and what the last check found.
type saved struct {
	Auto bool `json:"auto"`
	// Checks is "on" or "off" once the person answered; absent until then.
	Checks string `json:"checks,omitempty"`
	Latest string `json:"latest,omitempty"`
	App    *App   `json:"app,omitempty"`
}

func (m *Manager) load() saved {
	var s saved
	b, err := os.ReadFile(filepath.Join(m.Dir, stateFile))
	if err == nil && json.Unmarshal(b, &s) != nil {
		s = saved{}
	}
	return s
}

func (m *Manager) save(s saved) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.Dir, ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(m.Dir, stateFile))
}

// change applies f to update.json, one change at a time.
func (m *Manager) change(f func(*saved)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.load()
	f(&s)
	return m.save(s)
}

func (m *Manager) logf(f string, a ...any) {
	if m.Logf != nil {
		m.Logf(f, a...)
	}
}

// Latest is the newest released version the last check found, or "" before any.
func (m *Manager) Latest() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load().Latest
}

// App is the Android app in that release.
func (m *Manager) App() App {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.load().App; a != nil {
		return *a
	}
	return App{}
}

// Checks is whether the person wants the computer to ask GitHub about new releases each day: "ask" until they answer,
// then "on" or "off". Nothing is asked before "on": a program that phones home by itself looks like malware.
// 0.1.6 had no such question; whoever turned on its automatic updates said yes to checking.
func (m *Manager) Checks() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.load()
	switch {
	case s.Checks == "on" || s.Checks == "off":
		return s.Checks
	case s.Auto:
		return "on"
	}
	return "ask"
}

// SetChecks stores the answer. No also turns off installing by itself, which needs checking.
func (m *Manager) SetChecks(on bool) error {
	err := m.change(func(s *saved) {
		s.Checks = map[bool]string{true: "on", false: "off"}[on]
		s.Auto = s.Auto && on
	})
	if err == nil {
		m.poke()
	}
	return err
}

// Auto says the person wants new versions installed without asking. Off until they say so: this runs what a release
// publishes, on their computer, with no one looking.
func (m *Manager) Auto() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load().Auto
}

// SetAuto stores the person's choice. Installing by itself means checking, so on is a yes to that too.
func (m *Manager) SetAuto(on bool) error {
	err := m.change(func(s *saved) {
		s.Auto = on
		if on {
			s.Checks = "on"
		}
	})
	if err == nil {
		m.poke()
	}
	return err
}

// poke tells Run that the answer changed.
func (m *Manager) poke() {
	m.mu.Lock()
	w := m.wake
	m.mu.Unlock()
	if w != nil {
		select {
		case w <- struct{}{}:
		default:
		}
	}
}

// look asks GitHub once and keeps what it found, for phones and for after a restart.
func (m *Manager) look(ctx context.Context) (Release, error) {
	rel, err := m.Client.Latest(ctx)
	if err != nil {
		return rel, err
	}
	m.mu.Lock()
	m.looked = time.Now()
	m.mu.Unlock()
	app := rel.App
	return rel, m.change(func(s *saved) {
		s.Latest = rel.Version
		s.App = &app
		if app.URL == "" {
			s.App = nil
		}
	})
}

// CheckNow asks once, at the person's request, whatever the daily choice, and installs nothing.
func (m *Manager) CheckNow(ctx context.Context) (string, error) {
	rel, err := m.look(ctx)
	return rel.Version, err
}

// Check asks once, and with Auto on installs what it finds.
func (m *Manager) Check(ctx context.Context) {
	seen := m.Latest()
	rel, err := m.look(ctx)
	if err != nil {
		m.logf("update: can't check for a new version: %v", err)
		return
	}
	m.mu.Lock()
	tried := m.failed == rel.Version
	m.mu.Unlock()
	newer := Newer(rel.Version, m.Client.Current)
	if newer && seen != rel.Version {
		m.logf("update: %s is available (this is %s)", rel.Version, m.Client.Current)
	}
	if !newer || tried || !m.Installs || !m.Auto() {
		return
	}
	if err := m.install(ctx, rel); err != nil {
		m.mu.Lock()
		m.failed = rel.Version
		m.mu.Unlock()
		if !errors.Is(err, ErrBusy) {
			m.logf("update: couldn't install %s: %v", rel.Version, err)
		}
		return
	}
	m.logf("update: installed %s, restarting", rel.Version)
	if m.Restart != nil {
		m.Restart()
	}
}

// ApplyLatest installs the newest release, at a person's request, and returns its version. The caller restarts.
func (m *Manager) ApplyLatest(ctx context.Context) (string, error) {
	if !m.Installs {
		return "", ErrNotHere
	}
	rel, err := m.look(ctx)
	if err != nil {
		return "", err
	}
	if !Newer(rel.Version, m.Client.Current) {
		return "", ErrNoUpdate
	}
	if err := m.install(ctx, rel); err != nil {
		return "", err
	}
	m.logf("update: installed %s, restarting", rel.Version)
	return rel.Version, nil
}

func (m *Manager) install(ctx context.Context, rel Release) error {
	m.mu.Lock()
	if m.applying {
		m.mu.Unlock()
		return ErrBusy
	}
	m.applying = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.applying = false
		m.mu.Unlock()
	}()
	return m.Client.Apply(ctx, rel, m.Exe)
}

// Run asks after m.First and then every m.Every while the person's answer is yes, until ctx ends. A new answer counts
// from its own moment: a yes asks after m.First.
func (m *Manager) Run(ctx context.Context) {
	every := m.Every
	if every <= 0 {
		every = 24 * time.Hour
	}
	wake := make(chan struct{}, 1)
	m.mu.Lock()
	m.wake = wake
	m.mu.Unlock()
	wait := m.First
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			wait = m.First
			continue
		case <-time.After(wait):
		}
		wait = every
		if m.Checks() != "on" {
			continue
		}
		// A phone that just checked already asked; installing by itself still needs its own look.
		m.mu.Lock()
		since := time.Since(m.looked)
		m.mu.Unlock()
		if since < every && !m.Auto() {
			wait = every - since
			continue
		}
		m.Check(ctx)
	}
}
