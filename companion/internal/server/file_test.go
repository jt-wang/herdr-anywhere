package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The start of an MP4 (an ftyp box), of a PDF and of a QuickTime movie, as their files begin.
var (
	mp4Head = append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42\x00\x00\x00\x00mp42isom")...)
	movHead = append([]byte{0, 0, 0, 0x14}, []byte("ftypqt  \x00\x00\x00\x00qt  ")...)
	pdfHead = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n")
)

// fileFixture: the picture fixture's folders, plus a video, a movie, a PDF, notes in Markdown and a zip in the project.
func fileFixture(t *testing.T) *imageFix {
	t.Helper()
	x := imageFixture(t)
	proj := filepath.Join(x.home, "proj")
	for name, b := range map[string][]byte{
		"renders/demo.mp4": append(append([]byte{}, mp4Head...), make([]byte, 4000)...),
		"renders/take.mov": append(append([]byte{}, movHead...), make([]byte, 100)...),
		"docs/guide.pdf":   pdfHead,
		"NOTES.md":         []byte("# Notes\n\n- one\n"),
		"out.zip":          {'P', 'K', 3, 4, 0, 0},
	} {
		p := filepath.Join(proj, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(x.beyond, "secret.mp4"), mp4Head, 0o644)
	return x
}

// files asks what paths a reply names are, as POST /api/file/check answers.
func (x *imageFix) files(t *testing.T, terminal string, paths ...string) (int, []map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"terminal": terminal, "paths": paths})
	req, _ := http.NewRequest("POST", x.f.srv.URL+"/api/file/check", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if x.token != "" {
		req.Header.Set("Authorization", "Bearer "+x.token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body struct {
		Files []map[string]any `json:"files"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return r.StatusCode, body.Files
}

// A reply's paths, judged with the pictures' rules: what each file is (its bytes first, its name after), its name and
// size, and why one can't be opened.
func TestFileCheck(t *testing.T) {
	x := fileFixture(t)
	want := []struct{ asked, kind, name, why string }{
		{"../renders/demo.mp4", "video", "demo.mp4", ""},
		{"../renders/take.mov", "video", "take.mov", ""},
		{"../docs/guide.pdf", "pdf", "guide.pdf", ""},
		{"../NOTES.md", "text", "NOTES.md", ""},
		{"../shot.png", "image", "shot.png", ""},
		{"../out.zip", "other", "out.zip", ""},
		{"gone.mp4", "", "", "missing"},
		{"../dir.png", "", "", "not_file"},
		{filepath.Join(x.beyond, "secret.mp4"), "", "", "outside"},
		{"../out.png", "", "", "outside"},
	}
	var paths []string
	for _, w := range want {
		paths = append(paths, w.asked)
	}
	code, got := x.files(t, "t1", paths...)
	if code != 200 || len(got) != len(want) {
		t.Fatalf("%d, %d answers for %d paths: %v", code, len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		kind, _ := g["kind"].(string)
		name, _ := g["name"].(string)
		why, _ := g["why"].(string)
		if g["asked"] != w.asked || kind != w.kind || name != w.name || why != w.why {
			t.Errorf("%q: got kind %q name %q why %q, want %q %q %q", w.asked, kind, name, why, w.kind, w.name, w.why)
		}
		if w.why == "" && (g["size"] == nil || g["size"].(float64) <= 0) {
			t.Errorf("%q: no size: %v", w.asked, g)
		}
	}
	if got[0]["size"].(float64) != float64(len(mp4Head)+4000) {
		t.Errorf("the video's size: %v", got[0]["size"])
	}
}

func (x *imageFix) fetch(t *testing.T, path, terminal, rng string) (*http.Response, []byte) {
	t.Helper()
	q := url.Values{"path": {path}, "terminal": {terminal}}
	req, _ := http.NewRequest("GET", x.f.srv.URL+"/api/file?"+q.Encode(), nil)
	if x.token != "" {
		req.Header.Set("Authorization", "Bearer "+x.token)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r, b
}

// A file is sent as it is, in parts when asked (a player reads a video that way), with its type.
func TestFileSendsTheFileInParts(t *testing.T) {
	x := fileFixture(t)
	r, b := x.fetch(t, "../renders/demo.mp4", "t1", "bytes=4-11")
	if r.StatusCode != http.StatusPartialContent || string(b) != "ftypmp42" || r.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("%d %q %q", r.StatusCode, b, r.Header.Get("Content-Type"))
	}
	r, b = x.fetch(t, "../NOTES.md", "t1", "")
	if r.StatusCode != 200 || string(b) != "# Notes\n\n- one\n" || r.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q %q", r.StatusCode, b, r.Header.Get("Content-Type"))
	}
	for path, want := range map[string]int{
		"gone.mp4": http.StatusNotFound, "../dir.png": http.StatusUnsupportedMediaType,
		filepath.Join(x.beyond, "secret.mp4"): http.StatusForbidden, "../out.png": http.StatusForbidden,
	} {
		if r, _ := x.fetch(t, path, "t1", ""); r.StatusCode != want {
			t.Errorf("%q: %d, want %d", path, r.StatusCode, want)
		}
	}
	x.token = ""
	if r, _ := x.fetch(t, "../NOTES.md", "t1", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no paired device: %d", r.StatusCode)
	}
	if code, _ := x.files(t, "t1", "../NOTES.md"); code != http.StatusUnauthorized {
		t.Fatalf("a check without a paired device: %d", code)
	}
}

// link asks for a link to one file that a player can open without the device's token: POST /api/file/link.
func (x *imageFix) link(t *testing.T, path, terminal string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"terminal": terminal, "path": path})
	req, _ := http.NewRequest("POST", x.f.srv.URL+"/api/file/link", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if x.token != "" {
		req.Header.Set("Authorization", "Bearer "+x.token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return r.StatusCode, body.URL
}

func plain(t *testing.T, u, rng string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, b
}

// A video player can't send the device's token: it gets a link to that one file, which works while the device is
// paired and stops working a while after its last use.
func TestFileLinkOpensOneFileForAPlayer(t *testing.T) {
	x := fileFixture(t)
	code, u := x.link(t, "../renders/demo.mp4", "t1")
	if code != 200 || u == "" {
		t.Fatalf("%d %q", code, u)
	}
	if c, b := plain(t, x.f.srv.URL+u, "bytes=4-11"); c != http.StatusPartialContent || string(b) != "ftypmp42" {
		t.Fatalf("the link, read in parts: %d %q", c, b)
	}
	// The link is for that file only.
	other := strings.Replace(u, url.QueryEscape("../renders/demo.mp4"), url.QueryEscape("../NOTES.md"), 1)
	if other == u {
		t.Fatalf("the link doesn't name its path: %q", u)
	}
	if c, _ := plain(t, x.f.srv.URL+other, ""); c != http.StatusUnauthorized {
		t.Fatalf("the link opened another file: %d", c)
	}
	if c, _ := plain(t, x.f.srv.URL+"/api/file?path=..%2FNOTES.md&terminal=t1&key=made-up", ""); c != http.StatusUnauthorized {
		t.Fatalf("a made-up key: %d", c)
	}
	// No link for a file that can't be sent, nor for a device that isn't paired.
	if c, _ := x.link(t, "gone.mp4", "t1"); c != http.StatusNotFound {
		t.Fatalf("a link to a missing file: %d", c)
	}
	token := x.token
	x.token = ""
	if c, _ := x.link(t, "../renders/demo.mp4", "t1"); c != http.StatusUnauthorized {
		t.Fatalf("a link without a paired device: %d", c)
	}
	x.token = token
	// A while after its last use, the link stops working.
	x.f.s.fileLinkIdle = 50 * time.Millisecond
	_, u = x.link(t, "../renders/demo.mp4", "t1")
	time.Sleep(120 * time.Millisecond)
	if c, _ := plain(t, x.f.srv.URL+u, ""); c != http.StatusUnauthorized {
		t.Fatalf("an idle link still works: %d", c)
	}
}

// A device unpaired: its links stop working at once.
func TestFileLinkEndsWithItsDevice(t *testing.T) {
	x := fileFixture(t)
	id, token := x.f.pair(t)
	x.token = token
	_, u := x.link(t, "../renders/demo.mp4", "t1")
	if c, _ := plain(t, x.f.srv.URL+u, ""); c != 200 {
		t.Fatalf("before: %d", c)
	}
	if err := x.f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if c, _ := plain(t, x.f.srv.URL+u, ""); c != http.StatusUnauthorized {
		t.Fatalf("after the device was unpaired: %d", c)
	}
}
