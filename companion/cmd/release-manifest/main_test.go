package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/testfake"
	"herdr-anywhere/companion/internal/update"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runMain(args, &out)
	return out.String(), err
}

func TestKeygenKeepsTheKeyToItsOwner(t *testing.T) {
	key := filepath.Join(t.TempDir(), "update.key")
	out, err := run(t, "keygen", key)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(key)
	if err != nil || (testfake.PermBits() && fi.Mode().Perm() != 0o600) {
		t.Fatalf("key file %v, %v", fi, err)
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("printed %q", out)
	}
	before, _ := os.ReadFile(key)
	if _, err := run(t, "keygen", key); err == nil {
		t.Fatal("overwrote a key")
	}
	if after, _ := os.ReadFile(key); !bytes.Equal(before, after) {
		t.Fatal("the key changed")
	}
}

func TestSignWritesWhatACompanionAccepts(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	out, err := run(t, "keygen", key)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	mac := filepath.Join(dir, update.MacAsset)
	intel := filepath.Join(dir, intelMac)
	win := filepath.Join(dir, update.WindowsAsset)
	linux := filepath.Join(dir, update.LinuxAsset)
	linuxArm := filepath.Join(dir, update.LinuxArmAsset)
	apk := filepath.Join(dir, update.AppAsset)
	os.WriteFile(mac, []byte("mac file"), 0o755)
	os.WriteFile(intel, []byte("intel mac file"), 0o755)
	os.WriteFile(win, []byte("windows file"), 0o644)
	os.WriteFile(linux, []byte("linux file"), 0o755)
	os.WriteFile(linuxArm, []byte("linux arm file"), 0o755)
	os.WriteFile(apk, []byte("android file"), 0o644)
	rel := filepath.Join(dir, "out")
	os.Mkdir(rel, 0o755)
	if _, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", rel, "-any-key", mac, intel, win, linux, linuxArm, apk); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(rel, update.ManifestFile))
	sig, _ := os.ReadFile(filepath.Join(rel, update.ManifestFile+".sig"))
	m, err := update.Verify(ed25519.PublicKey(pub), body, sig)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("mac file"))
	want := update.File{Name: update.MacAsset, SHA256: hex.EncodeToString(sum[:]), Bytes: 8}
	names := []string{update.MacAsset, intelMac, update.WindowsAsset, update.LinuxAsset, update.LinuxArmAsset, update.AppAsset}
	if m.Version != "0.1.8" || len(m.Files) != len(names) || m.Files[0] != want {
		t.Fatalf("%+v", m)
	}
	for i, n := range names {
		if m.Files[i].Name != n {
			t.Errorf("file %d is %q, want %q", i, m.Files[i].Name, n)
		}
	}
}

// The Intel Mac's file, as an Intel Mac's companion looks for it (update.AssetFor("darwin", "amd64")).
const intelMac = "herdr-anywhere-macos-amd64"

// A release that leaves out one computer's file, such as the Intel Mac's when a release script built only the others,
// would tell that computer of a version it can't install. sign refuses it unless asked for part of a release (-only,
// for tests that need one computer's file).
func TestSignRefusesAReleaseMissingAComputersFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	var files []string
	for _, n := range update.ReleaseFiles {
		if n == intelMac {
			continue
		}
		p := filepath.Join(dir, n)
		os.WriteFile(p, []byte(n), 0o644)
		files = append(files, p)
	}
	if len(files) != len(update.ReleaseFiles)-1 {
		t.Fatalf("a release's files are %v, without %s", update.ReleaseFiles, intelMac)
	}
	out, err := run(t, append([]string{"sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key"}, files...)...)
	if err == nil || !strings.Contains(err.Error(), intelMac) {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
	}
	if _, err := run(t, append([]string{"sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key", "-only"}, files...)...); err != nil {
		t.Fatalf("-only: %v", err)
	}
}

// A file under a name no computer looks for (here Herdr's own spelling of the machine) would leave those computers
// without the update, so sign refuses it.
func TestSignRefusesAFileNoComputerLooksFor(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	wrong := filepath.Join(dir, "herdr-anywhere-linux-x86_64")
	os.WriteFile(wrong, []byte("x"), 0o755)
	out, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key", wrong)
	if err == nil || !strings.Contains(err.Error(), "herdr-anywhere-linux-x86_64") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
	}
}

// Signing with a key computers don't have would leave them refusing every later release.
func TestSignRefusesAKeyTheCompanionDoesNotTrust(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if _, err := run(t, "keygen", key); err != nil {
		t.Fatal(err)
	}
	// Every computer's file, so the key is the only thing wrong.
	var files []string
	for _, n := range update.ReleaseFiles {
		files = append(files, filepath.Join(dir, n))
		os.WriteFile(files[len(files)-1], []byte(n), 0o644)
	}
	args := append([]string{"sign", "-key", key, "-version", "0.1.8", "-out", dir}, files...)
	if _, err := run(t, args...); err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("signed with a key the companion doesn't trust: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
	}
}

func TestSignRefusesAVersionThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	mac := filepath.Join(dir, update.MacAsset)
	os.WriteFile(mac, []byte("x"), 0o755)
	if out, err := run(t, "sign", "-key", key, "-version", "v0.1", "-out", dir, "-any-key", "-only", mac); err == nil || !strings.Contains(err.Error(), "v0.1") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestVerifyUsesTheBuiltInKeyAndExactBytes(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "update.key")
	if _, err := run(t, "keygen", keyPath); err != nil {
		t.Fatal(err)
	}
	key, err := readKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	body, sig, err := update.Sign(key, update.Manifest{Version: "0.3.1"})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "latest.json")
	sigPath := manifestPath + ".sig"
	if err := os.WriteFile(manifestPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "verify", manifestPath, sigPath); err == nil || !strings.Contains(err.Error(), "isn't signed") {
		t.Fatalf("accepted an untrusted key, or failed for the wrong reason: %v", err)
	}
	// Only the test replaces the embedded key; the command offers no key override.
	original := update.PublicKey
	t.Cleanup(func() { update.PublicKey = original })
	update.PublicKey = key.Public().(ed25519.PublicKey)
	if out, err := run(t, "verify", manifestPath, sigPath); err != nil || !strings.Contains(out, "verified 0.3.1") {
		t.Fatalf("valid signature: %q, %v", out, err)
	}
	if err := os.WriteFile(manifestPath, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "verify", manifestPath, sigPath); err == nil {
		t.Fatal("accepted modified manifest bytes")
	}
	if err := os.WriteFile(manifestPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, []byte("not a signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "verify", manifestPath, sigPath); err == nil {
		t.Fatal("accepted malformed signature")
	}
}

func TestVerifyRequiresBothFiles(t *testing.T) {
	for _, args := range [][]string{{"verify"}, {"verify", "latest.json"}, {"verify", "a", "b", "c"}} {
		if _, err := run(t, args...); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if _, err := run(t, "verify", filepath.Join(t.TempDir(), "missing"), "missing.sig"); err == nil {
		t.Fatal("accepted missing files")
	}
}

// Execute the workflow's actual trust check against a disposable real Git repository.
func TestReleaseSigningWorkflowTrustChecks(t *testing.T) {
	for _, tool := range []string{"python3", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("workflow regression needs %s", tool)
		}
	}
	workflow, err := os.ReadFile("../../../.github/workflows/release-signing.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, script, ok := strings.Cut(strings.ReplaceAll(string(workflow), "\r\n", "\n"), "python3 - <<'PY_REQUEST'\n")
	if !ok {
		t.Fatal("missing workflow request check")
	}
	script, _, ok = strings.Cut(script, "          PY_REQUEST\n")
	if !ok {
		t.Fatal("missing request check terminator")
	}
	script = strings.ReplaceAll(script, "\n          ", "\n")
	script = strings.TrimPrefix(script, "          ")
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("config", "user.name", "Release Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("config", "tag.gpgsign", "false")
	git("commit", "--allow-empty", "-m", "older commit")
	git("tag", "v0.3.3")
	if err := os.Mkdir(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "app", "nsh.json")
	if err := os.WriteFile(config, []byte(`{"version":"0.3.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "app/nsh.json")
	git("commit", "-m", "release commit")
	sha := git("rev-parse", "HEAD")
	git("tag", "-a", "v0.3.1", "-m", "stable", "--", "HEAD")
	git("tag", "v0.3.2")
	git("remote", "add", "origin", dir)
	git("checkout", "--detach", sha)
	trusted := map[string]string{
		"RELEASE_EXPECTED_SHA": sha, "RELEASE_TAG": "v0.3.1", "RELEASE_DEFAULT_BRANCH": "main",
		"GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_REPOSITORY": "jt-wang/herdr-anywhere",
		"GITHUB_REF": "refs/tags/v0.3.1", "GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v0.3.1",
		"GITHUB_SHA": sha, "GITHUB_WORKFLOW_SHA": sha,
		"GITHUB_WORKFLOW_REF": "jt-wang/herdr-anywhere/.github/workflows/release-signing.yml@refs/tags/v0.3.1",
	}
	check := func(overrides map[string]string, wantOK bool) {
		t.Helper()
		cmd := exec.Command("python3", "-c", script)
		cmd.Dir = dir
		// Keep PATH for Git, but do not inherit CI's GitHub context or signing secrets.
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		for k, v := range trusted {
			if value, ok := overrides[k]; ok {
				v = value
			}
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("overrides=%v: success=%v, want %v: %s", overrides, err == nil, wantOK, out)
		}
	}
	check(nil, true)
	for key, values := range map[string][]string{
		"GITHUB_EVENT_NAME":      {"pull_request", "pull_request_target", "push"},
		"GITHUB_REPOSITORY":      {"someone/herdr-anywhere"},
		"GITHUB_REF":             {"refs/heads/main", "refs/heads/v0.3.1", "refs/heads/feature"},
		"GITHUB_REF_TYPE":        {"branch"},
		"GITHUB_REF_NAME":        {"v0.3.2"},
		"GITHUB_WORKFLOW_REF":    {"jt-wang/herdr-anywhere/.github/workflows/other.yml@refs/tags/v0.3.1", "jt-wang/herdr-anywhere/.github/workflows/release-signing.yml@refs/heads/main"},
		"GITHUB_WORKFLOW_SHA":    {strings.Repeat("0", 40)},
		"GITHUB_SHA":             {strings.Repeat("0", 40)},
		"RELEASE_EXPECTED_SHA":   {"main", strings.Repeat("0", 40)},
		"RELEASE_DEFAULT_BRANCH": {""},
		"RELEASE_TAG":            {"v0.3.1-pre.1", "v0.3.1+build", "ios-build-36", "0.3.1", "v00.3.1", "v0.3", "v0.3.2", "v0.3.3", "v9.9.9", "--help"},
	} {
		for _, value := range values {
			check(map[string]string{key: value}, false)
		}
	}
	// Even internally consistent branch context cannot masquerade as a version tag.
	check(map[string]string{
		"GITHUB_REF": "refs/heads/v0.3.1", "GITHUB_REF_TYPE": "branch",
		"GITHUB_WORKFLOW_REF": "jt-wang/herdr-anywhere/.github/workflows/release-signing.yml@refs/heads/v0.3.1",
	}, false)
	for _, tag := range []string{"v0.3.1-pre.1", "v0.3.1+build", "ios-build-36", "v00.3.1", "v0.3"} {
		git("tag", tag)
		check(map[string]string{
			"RELEASE_TAG": tag, "GITHUB_REF_NAME": tag, "GITHUB_REF": "refs/tags/" + tag,
			"GITHUB_WORKFLOW_REF": "jt-wang/herdr-anywhere/.github/workflows/release-signing.yml@refs/tags/" + tag,
		}, false)
	}
	// Keep version/context valid, changing only the tag's target; then accept a lightweight tag too.
	git("tag", "-f", "v0.3.1", "HEAD^")
	check(nil, false)
	git("tag", "-f", "v0.3.1", "HEAD")
	check(nil, true)
	// Exact tagged code must already be on the public default branch, not merely share history.
	git("update-ref", "refs/heads/main", sha+"^")
	check(nil, false)
	git("update-ref", "refs/heads/main", sha)
	git("checkout", "main")
	git("commit", "--allow-empty", "-m", "newer reviewed commit")
	git("checkout", "--detach", sha)
	check(nil, true)
	if err := os.WriteFile(config, []byte(`{"version":"0.3.2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	check(nil, false)
}

func TestReleaseSigningWorkflowAllowsPrereleaseObject(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("workflow regression needs python3")
	}
	workflow, err := os.ReadFile("../../../.github/workflows/release-signing.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, script, ok := strings.Cut(strings.ReplaceAll(string(workflow), "\r\n", "\n"), "python3 - <<'PY_RELEASE'\n")
	if !ok {
		t.Fatal("missing workflow release check")
	}
	script, _, ok = strings.Cut(script, "          PY_RELEASE\n")
	if !ok {
		t.Fatal("missing release check terminator")
	}
	script = strings.TrimPrefix(strings.ReplaceAll(script, "\n          ", "\n"), "          ")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "release-signing"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"tagName":"v0.3.1","isDraft":false,"isPrerelease":true}`, true},
		{`{"tagName":"v0.3.1","isDraft":false,"isPrerelease":false}`, true},
		{`{"tagName":"v0.3.1","isDraft":true,"isPrerelease":true}`, false},
		{`{"tagName":"v0.3.2","isDraft":false,"isPrerelease":true}`, false},
	} {
		if err := os.WriteFile(filepath.Join(dir, "release-signing", "release.json"), []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("python3", "-c", script)
		cmd.Env = []string{"RUNNER_TEMP=" + dir, "RELEASE_TAG=v0.3.1"}
		out, err := cmd.CombinedOutput()
		if (err == nil) != tc.ok {
			t.Fatalf("%s: success=%v, want %v: %s", tc.body, err == nil, tc.ok, out)
		}
	}
}
