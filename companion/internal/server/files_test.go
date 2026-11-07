package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

func upload(t *testing.T, f *fixture, token, name, on string, body []byte) (int, map[string]any) {
	t.Helper()
	q := "?name=" + url.QueryEscape(name)
	if on != "" {
		q += "&on=" + url.QueryEscape(on)
	}
	req, _ := http.NewRequest("POST", f.srv.URL+"/api/files"+q, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}

func TestFilesNeedAPairedDevice(t *testing.T) {
	f, home, _ := folderFixture(t)
	if code, _ := upload(t, f, "", "a.png", "", []byte("x")); code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", code)
	}
	if code, _ := upload(t, f, "nope", "a.png", "", []byte("x")); code != http.StatusUnauthorized {
		t.Fatalf("bad token: status %d", code)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache")); err == nil {
		t.Fatal("wrote something for an unknown device")
	}
}

func TestFileLandsPrivatelyAndSaysWhere(t *testing.T) {
	f, home, token := folderFixture(t)
	body := bytes.Repeat([]byte("0123456789"), 3_000_000) // 30 MB: past every WebSocket limit on the way
	code, out := upload(t, f, token, "report final.pdf", "", body)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	p, _ := out["path"].(string)
	if !strings.HasPrefix(p, filepath.Join(home, ".cache", "herdr-anywhere", "uploads")+string(filepath.Separator)) || filepath.Base(p) != "report_final.pdf" {
		t.Fatalf("path %q", p)
	}
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read back %d bytes, err %v", len(got), err)
	}
	if out["bytes"] != float64(len(body)) {
		t.Fatalf("bytes %v", out["bytes"])
	}
	if st, _ := os.Stat(filepath.Dir(p)); testfake.PermBits() && st.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", st.Mode().Perm())
	}
}

func TestFileNameCannotLeaveItsFolder(t *testing.T) {
	f, home, token := folderFixture(t)
	for _, name := range []string{"../../../evil.txt", "..", "", "/etc/passwd", "a\x00b"} {
		code, out := upload(t, f, token, name, "", []byte("x"))
		if code != 200 {
			t.Fatalf("%q: status %d", name, code)
		}
		p := out["path"].(string)
		rel, err := filepath.Rel(filepath.Join(home, ".cache", "herdr-anywhere", "uploads"), p)
		if err != nil || strings.HasPrefix(rel, "..") || strings.Count(filepath.ToSlash(rel), "/") != 1 { // one folder, then the file
			t.Fatalf("%q landed at %q", name, p)
		}
	}
}

func TestSameNameTwiceKeepsBoth(t *testing.T) {
	f, _, token := folderFixture(t)
	_, a := upload(t, f, token, "a.png", "", []byte("one"))
	_, b := upload(t, f, token, "a.png", "", []byte("two"))
	if a["path"] == b["path"] {
		t.Fatalf("both at %v", a["path"])
	}
}

func TestOldUploadsGoAway(t *testing.T) {
	f, _, token := folderFixture(t)
	_, old := upload(t, f, token, "old.png", "", []byte("x"))
	dir := filepath.Dir(old["path"].(string))
	week := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(dir, week, week); err != nil {
		t.Fatal(err)
	}
	_, fresh := upload(t, f, token, "new.png", "", []byte("x"))
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the old upload is still there: %v", err)
	}
	if _, err := os.Stat(fresh["path"].(string)); err != nil {
		t.Fatal(err)
	}
}

func TestFileOverTheLimitLeavesNothing(t *testing.T) {
	f, home, token := folderFixture(t)
	f.s.maxUpload = 10
	code, _ := upload(t, f, token, "big.bin", "", bytes.Repeat([]byte("x"), 11))
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", code)
	}
	left, _ := os.ReadDir(filepath.Join(home, ".cache", "herdr-anywhere", "uploads"))
	if len(left) != 0 {
		t.Fatalf("left %d folders behind", len(left))
	}
}

// fakeExecHC runs `hc exec [-i] --on m --timeout n ARGV...` here, with $REMOTE as its home, as hc runs it there.
func fakeExecHC(t *testing.T, remote string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hc")
	script := `#!/bin/sh
[ "$1" = exec ] || exit 9
shift
while :; do case "$1" in
-i) shift ;;
--on|--timeout) shift 2 ;;
*) break ;;
esac; done
HOME=` + remote + ` exec "$@"
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileForAnotherMachineGoesThereInPieces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the other machine runs the sh script hc exec sends; this fake runs it here, and Windows has no sh")
	}
	remote := t.TempDir()
	f, home, token := folderFixture(t)
	f.s.cfg.HC = fakeExecHC(t, remote)
	f.s.machineChunk = 7 // pieces hc takes: it refuses more than 8 MB piped in at once
	body := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	code, out := upload(t, f, token, "report.pdf", "box", body)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	p := out["path"].(string)
	if !strings.HasPrefix(p, filepath.Join(remote, ".cache", "herdr-anywhere", "uploads")+string(filepath.Separator)) {
		t.Fatalf("path %q", p)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, body) {
		t.Fatalf("there: %q", got)
	}
	left, _ := os.ReadDir(filepath.Join(home, ".cache", "herdr-anywhere", "uploads"))
	if len(left) != 0 {
		t.Fatalf("left %d folders on this computer", len(left))
	}
}

func TestFileForAMachineThatFailsSaysSo(t *testing.T) {
	f, _, token := folderFixture(t)
	f.s.cfg.HC = testfake.HC(t, t.TempDir(), "hc", testfake.Spec{Exit: 1})
	if code, out := upload(t, f, token, "a.png", "box", []byte("x")); code != http.StatusBadGateway {
		t.Fatalf("status %d: %v", code, out)
	}
}
