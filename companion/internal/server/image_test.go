package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/testfake"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 4, 3))
	m.Set(1, 1, color.RGBA{0, 0, 255, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type imageFix struct {
	f                       *fixture
	token                   string
	home, work, tmp, beyond string
	png                     []byte
}

// imageFixture: a home with a project; t1 is a terminal whose program is in ~/proj/sub; a temp folder; a folder
// outside all of them. Every folder is real (symlinks resolved), as the server answers with real paths.
// hcGet is what `hc get ADDR --json` prints for a terminal that started in cwd and whose program (claude) is now in
// fg ("": no program). Marshalled, as hc does: a path's backslashes and quotes are escaped.
func hcGet(t *testing.T, addr, cwd, fg string) []byte {
	t.Helper()
	v := map[string]any{"address": addr, "cwd": cwd}
	if fg != "" {
		v["foreground"] = map[string]any{"pid": 9, "name": "claude", "cwd": fg}
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rel is the way from one folder to another with ".." (here: from the home up and over to a folder outside it).
func (x *imageFix) rel(t *testing.T, from, to string) string {
	t.Helper()
	r, err := filepath.Rel(from, to)
	if err != nil || !strings.HasPrefix(r, "..") {
		t.Fatalf("no way up from %q to %q: %q %v", from, to, r, err)
	}
	return r
}

func imageFixture(t *testing.T) *imageFix {
	t.Helper()
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// The home's name has a backslash and a quote: what herdr-core says about it must come through as JSON says it.
	// Windows allows neither in a name, but its paths are full of backslashes already.
	name := `Sam's "home" \ here`
	if runtime.GOOS == "windows" {
		name = "Sam's home"
	}
	home := filepath.Join(real(t.TempDir()), name)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	x := &imageFix{home: home, tmp: real(t.TempDir()), beyond: real(t.TempDir()), png: pngBytes(t)}
	x.work = filepath.Join(x.home, "proj", "sub")
	write := func(p string, b []byte) {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(x.home, "proj", "shot.png"), x.png)
	write(filepath.Join(x.work, "shots", "Home.JPEG"), x.png) // a name may say one type and the file be another
	write(filepath.Join(x.home, "Desktop", "Screenshot of the home page at 10.00.00.png"), x.png)
	write(filepath.Join(x.home, "proj", "notes.png"), []byte("not a picture, only named like one\n"))
	write(filepath.Join(x.home, "proj", "big.png"), append(append([]byte{}, x.png...), make([]byte, 4096)...))
	write(filepath.Join(x.tmp, "cap.png"), x.png)
	write(filepath.Join(x.beyond, "secret.png"), x.png)
	os.MkdirAll(filepath.Join(x.home, "proj", "dir.png"), 0o755)
	os.Symlink(filepath.Join(x.beyond, "secret.png"), filepath.Join(x.home, "proj", "out.png"))
	os.Symlink(filepath.Join(x.home, "proj", "shot.png"), filepath.Join(x.home, "proj", "same.png"))
	os.Symlink(x.beyond, filepath.Join(x.home, "proj", "away"))

	d := t.TempDir()
	write(filepath.Join(d, "get-t1.json"), hcGet(t, "t1", x.home, x.work))
	write(filepath.Join(d, "get-t2.json"), hcGet(t, "t2", x.beyond, ""))
	hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{Version: "hc 0.1.19", On: map[string][]testfake.Reply{
		"get": {{Cat: filepath.Join(d, "get-{2}.json")}},
	}, Else: 1})
	x.f = newFixture(t, func(c *Config) { c.Home = x.home; c.HC = hc; c.ImageTemp = []string{x.tmp} })
	x.f.s.maxImage = int64(len(x.png) + 1024)
	_, x.token = x.f.pair(t)
	return x
}

func (x *imageFix) get(t *testing.T, path, terminal string) (int, map[string]any, http.Header) {
	t.Helper()
	q := url.Values{"path": {path}}
	if terminal != "" {
		q.Set("terminal", terminal)
	}
	req, _ := http.NewRequest("GET", x.f.srv.URL+"/api/image?"+q.Encode(), nil)
	if x.token != "" {
		req.Header.Set("Authorization", "Bearer "+x.token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	return r.StatusCode, body, r.Header
}

// ok checks a 200 that carries the picture's own bytes, said to be at the full path asked for.
func (x *imageFix) ok(t *testing.T, path, terminal, want string) {
	t.Helper()
	code, body, h := x.get(t, path, terminal)
	if code != 200 {
		t.Fatalf("%q: %d %v", path, code, body)
	}
	u, _ := body["url"].(string)
	b64, found := strings.CutPrefix(u, "data:image/png;base64,")
	got, _ := base64.StdEncoding.DecodeString(b64)
	if !found || !bytes.Equal(got, x.png) {
		t.Fatalf("%q: url %.40q…, want the PNG's bytes", path, u)
	}
	if body["path"] != want {
		t.Fatalf("%q: path %v, want %s", path, body["path"], want)
	}
	if h.Get("Cache-Control") != "no-store" {
		t.Fatalf("%q: Cache-Control %q: the phone keeps no copy", path, h.Get("Cache-Control"))
	}
}

// refused checks the refusal and returns why it says, as a code.
func (x *imageFix) refused(t *testing.T, path, terminal string, want int) string {
	t.Helper()
	code, body, _ := x.get(t, path, terminal)
	if code != want {
		t.Fatalf("%q: %d %v, want %d", path, code, body, want)
	}
	if _, has := body["url"]; has {
		t.Fatalf("%q: refused but sent a picture", path)
	}
	if e, _ := body["error"].(string); e == "" {
		t.Fatalf("%q: no error said", path)
	}
	why, _ := body["why"].(string)
	return why
}

func TestImageNeedsAPairedDevice(t *testing.T) {
	x := imageFixture(t)
	x.token = ""
	x.refused(t, filepath.Join(x.home, "proj", "shot.png"), "", http.StatusUnauthorized)
	x.token = "not-a-token"
	x.refused(t, filepath.Join(x.home, "proj", "shot.png"), "", http.StatusUnauthorized)
}

func TestImageReadsAPictureByItsPath(t *testing.T) {
	x := imageFixture(t)
	shot := filepath.Join(x.home, "proj", "shot.png")
	x.ok(t, shot, "", shot)
	x.ok(t, "~/proj/shot.png", "", shot)
	x.ok(t, filepath.Join(x.home, "Desktop", "Screenshot of the home page at 10.00.00.png"), "t1", filepath.Join(x.home, "Desktop", "Screenshot of the home page at 10.00.00.png"))
	// A relative one is in the folder the terminal's program is in now, and its type is what the file is.
	x.ok(t, "shots/Home.JPEG", "t1", filepath.Join(x.work, "shots", "Home.JPEG"))
	x.ok(t, "../shot.png", "t1", shot)
	// Temp folders, where tools put screenshots; a symlink to a picture inside counts as that picture.
	x.ok(t, filepath.Join(x.tmp, "cap.png"), "", filepath.Join(x.tmp, "cap.png"))
	x.ok(t, filepath.Join(x.home, "proj", "same.png"), "", filepath.Join(x.home, "proj", "same.png"))
}

func TestImageInTheTerminalsFolderOutsideHome(t *testing.T) {
	x := imageFixture(t)
	// t2 works in a folder outside home: what is in it may be read through t2 only.
	x.ok(t, "secret.png", "t2", filepath.Join(x.beyond, "secret.png"))
	x.refused(t, filepath.Join(x.beyond, "secret.png"), "", http.StatusForbidden)
	x.refused(t, filepath.Join(x.beyond, "secret.png"), "t1", http.StatusForbidden)
}

func TestImageRefusals(t *testing.T) {
	x := imageFixture(t)
	for _, c := range []struct {
		why, path, terminal string
		code                int
		says                string
	}{
		{"outside home, the terminal's folder and temp", filepath.Join(x.beyond, "secret.png"), "", http.StatusForbidden, "outside"},
		{"out of home by ..", "~/" + filepath.ToSlash(x.rel(t, x.home, x.beyond)) + "/secret.png", "", http.StatusForbidden, "outside"},
		{"a symlink to a picture outside", filepath.Join(x.home, "proj", "out.png"), "", http.StatusForbidden, "outside"},
		{"through a symlinked folder that leads outside", filepath.Join(x.home, "proj", "away", "secret.png"), "", http.StatusForbidden, "outside"},
		{"not a picture, though named like one", filepath.Join(x.home, "proj", "notes.png"), "", http.StatusUnsupportedMediaType, "not_image"},
		{"a folder named like a picture", filepath.Join(x.home, "proj", "dir.png"), "", http.StatusUnsupportedMediaType, "not_image"},
		{"too big", filepath.Join(x.home, "proj", "big.png"), "", http.StatusRequestEntityTooLarge, "too_big"},
		{"no such file", filepath.Join(x.home, "proj", "gone.png"), "", http.StatusNotFound, "missing"},
		{"relative, and no terminal to say where", "shots/Home.JPEG", "", http.StatusBadRequest, "relative"},
		{"a terminal herdr-core doesn't know", "shots/Home.JPEG", "t404", http.StatusNotFound, "no_terminal"},
		{"no path", "", "t1", http.StatusBadRequest, "path"},
		{"a path longer than a TUI shows", "/" + strings.Repeat("a", 1100) + ".png", "", http.StatusBadRequest, "path"},
		{"a line break in the path", "a\ntouch x\n.png", "t1", http.StatusBadRequest, "path"},
		{"a control character in the path", "a\x1b[2J.png", "t1", http.StatusBadRequest, "path"},
	} {
		t.Run(c.why, func(t *testing.T) {
			if why := x.refused(t, c.path, c.terminal, c.code); why != c.says {
				t.Fatalf("why %q, want %q", why, c.says)
			}
		})
	}
}

// remoteFixture: machine "box" (its home a folder here, as the fake hc runs `exec --on box` in it) with the same
// files as imageFixture's home; box/t3 is a terminal whose program is in ~/proj/sub there. Machine "gone" has a
// terminal but can't be reached.
func remoteFixture(t *testing.T) *imageFix {
	t.Helper()
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	root := real(t.TempDir())
	x := &imageFix{home: filepath.Join(root, "box"), tmp: real(t.TempDir()), beyond: real(t.TempDir()), png: pngBytes(t)}
	x.work = filepath.Join(x.home, "proj", "sub")
	write := func(p string, b []byte) {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(x.home, "proj", "shot.png"), x.png)
	write(filepath.Join(x.work, "shots", "Home.JPEG"), x.png)
	write(filepath.Join(x.home, "Desktop", "Screenshot of the home page at 10.00.00.png"), x.png)
	write(filepath.Join(x.home, "proj", "notes.png"), []byte("not a picture, only named like one\n"))
	write(filepath.Join(x.home, "proj", "big.png"), append(append([]byte{}, x.png...), make([]byte, 4096)...))
	write(filepath.Join(x.tmp, "cap.png"), x.png)
	write(filepath.Join(x.beyond, "secret.png"), x.png)
	os.MkdirAll(filepath.Join(x.home, "proj", "dir.png"), 0o755)
	os.Symlink(filepath.Join(x.beyond, "secret.png"), filepath.Join(x.home, "proj", "out.png"))
	os.Symlink("shot.png", filepath.Join(x.home, "proj", "same.png"))
	os.Symlink("../shot.png", filepath.Join(x.work, "up.png"))
	os.Symlink(x.beyond, filepath.Join(x.home, "proj", "away"))

	d := t.TempDir()
	write(filepath.Join(d, "get-box", "t3.json"), hcGet(t, "box/t3", x.home, x.work))
	write(filepath.Join(d, "get-gone", "t1.json"), []byte(`{"address":"gone/t1","cwd":"/home/sam"}`))
	// What hc 0.1.19 says on stderr, exit 5, for a terminal on a machine it can't reach now (measured:
	// e2e/remote-picture.sh's setup, the machine's container removed).
	write(filepath.Join(d, "fail-down", "t1.json"), []byte(`{"error":{"hint":"hc machine ls shows when it comes back","kind":"unavailable","message":"down is unreachable"}}`))
	hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{Version: "hc 0.1.19", On: map[string][]testfake.Reply{
		"get":  {{If: filepath.Join(d, "fail-{2}.json"), Fail: filepath.Join(d, "fail-{2}.json")}, {Cat: filepath.Join(d, "get-{2}.json")}},
		"exec": {{Exec: root}},
	}, Else: 1})
	x.f = newFixture(t, func(c *Config) { c.Home = real(t.TempDir()); c.HC = hc; c.ImageTemp = []string{real(t.TempDir())} })
	x.f.s.maxImage = int64(len(x.png) + 1024)
	x.f.s.remoteTemp = []string{x.tmp}
	_, x.token = x.f.pair(t)
	return x
}

func TestImageOnAnotherMachine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the check runs in sh on that machine")
	}
	x := remoteFixture(t)
	shot := filepath.Join(x.home, "proj", "shot.png")
	x.ok(t, shot, "box/t3", shot)
	x.ok(t, "~/proj/shot.png", "box/t3", shot)
	x.ok(t, "shots/Home.JPEG", "box/t3", filepath.Join(x.work, "shots", "Home.JPEG"))
	x.ok(t, "../shot.png", "box/t3", shot)
	x.ok(t, "~/Desktop/Screenshot of the home page at 10.00.00.png", "box/t3", filepath.Join(x.home, "Desktop", "Screenshot of the home page at 10.00.00.png"))
	x.ok(t, filepath.Join(x.home, "proj", "same.png"), "box/t3", filepath.Join(x.home, "proj", "same.png"))
	x.ok(t, "up.png", "box/t3", filepath.Join(x.work, "up.png"))
	x.ok(t, filepath.Join(x.tmp, "cap.png"), "box/t3", filepath.Join(x.tmp, "cap.png"))
}

func TestImageOnAnotherMachineRefusals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the check runs in sh on that machine")
	}
	x := remoteFixture(t)
	for _, c := range []struct {
		why, path, terminal string
		code                int
		says                string
	}{
		{"outside home, the terminal's folder and temp", filepath.Join(x.beyond, "secret.png"), "box/t3", http.StatusForbidden, "outside"},
		{"out of home by ..", "~/../../" + filepath.Base(x.beyond) + "/secret.png", "box/t3", http.StatusForbidden, "outside"},
		{"a symlink to a picture outside", "~/proj/out.png", "box/t3", http.StatusForbidden, "outside"},
		{"through a symlinked folder that leads outside", "~/proj/away/secret.png", "box/t3", http.StatusForbidden, "outside"},
		{"not a picture, though named like one", "~/proj/notes.png", "box/t3", http.StatusUnsupportedMediaType, "not_image"},
		{"a folder named like a picture", "~/proj/dir.png", "box/t3", http.StatusUnsupportedMediaType, "not_image"},
		{"too big", "~/proj/big.png", "box/t3", http.StatusRequestEntityTooLarge, "too_big"},
		{"no such file", "~/proj/gone.png", "box/t3", http.StatusNotFound, "missing"},
		{"a terminal herdr-core doesn't know", "shots/Home.JPEG", "box/t404", http.StatusNotFound, "no_terminal"},
		{"a machine that can't be reached", "/home/sam/a.png", "gone/t1", http.StatusBadGateway, "unreachable"},
		{"a machine hc can't reach now", "~/a.png", "down/t1", http.StatusBadGateway, "unreachable"},
		{"a machine hc doesn't know", "~/a.png", "nosuch/t1", http.StatusNotFound, "no_terminal"},
		{"a line break in the path", "a\ntouch x\n.png", "box/t3", http.StatusBadRequest, "path"},
	} {
		t.Run(c.why, func(t *testing.T) {
			if why := x.refused(t, c.path, c.terminal, c.code); why != c.says {
				t.Fatalf("why %q, want %q", why, c.says)
			}
		})
	}
}

// hc keeps the first 8 MB a program on another machine writes: a picture bigger than that is refused there, not
// sent cut short.
func TestImageOnAnotherMachineIsAtMostWhatHcCarries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the check runs in sh on that machine")
	}
	x := remoteFixture(t)
	x.f.s.maxImage = 12 << 20
	big := append(append([]byte{}, x.png...), make([]byte, 9<<20)...)
	if err := os.WriteFile(filepath.Join(x.home, "proj", "huge.png"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if why := x.refused(t, "~/proj/huge.png", "box/t3", http.StatusRequestEntityTooLarge); why != "too_big" {
		t.Fatalf("why %q, want too_big", why)
	}
	if _, body, _ := x.get(t, "~/proj/huge.png", "box/t3"); body["limit"] != float64(8<<20) {
		t.Fatalf("limit %v, want the 8 MB hc carries", body["limit"])
	}
}

// A picture too big says the limit, for the phone to say it.
func TestImageTooBigSaysTheLimit(t *testing.T) {
	x := imageFixture(t)
	if _, body, _ := x.get(t, filepath.Join(x.home, "proj", "big.png"), ""); body["limit"] != float64(x.f.s.maxImage) {
		t.Fatalf("limit %v, want %d", body["limit"], x.f.s.maxImage)
	}
}

// check asks which of paths, as a terminal shows them, are pictures: POST /api/image/check.
func (x *imageFix) check(t *testing.T, terminal string, paths ...string) (int, []map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"terminal": terminal, "paths": paths})
	req, _ := http.NewRequest("POST", x.f.srv.URL+"/api/image/check", bytes.NewReader(b))
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
		Paths []map[string]any `json:"paths"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return r.StatusCode, body.Paths
}

// checked runs check and compares, path by path in order, whether it is a picture and why not.
func (x *imageFix) checked(t *testing.T, terminal string, want [][3]string) {
	t.Helper()
	var paths []string
	for _, w := range want {
		paths = append(paths, w[0])
	}
	code, got := x.check(t, terminal, paths...)
	if code != 200 || len(got) != len(want) {
		t.Fatalf("%d, %d answers for %d paths: %v", code, len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		pic, _ := g["picture"].(bool)
		why, _ := g["why"].(string)
		if g["asked"] != w[0] || fmt.Sprint(pic) != w[1] || why != w[2] {
			t.Errorf("%q: asked %v picture %v why %q, want picture %s why %q", w[0], g["asked"], pic, why, w[1], w[2])
		}
		if _, has := g["url"]; has {
			t.Errorf("%q: a check sends no picture", w[0])
		}
	}
}

// The tap and the underline only go to what is a picture on that computer: a check answers for many paths at once,
// with the same rules as the picture itself, and reads no more than a file's first bytes.
func TestImageCheck(t *testing.T) {
	x := imageFixture(t)
	x.checked(t, "t1", [][3]string{
		{"../shot.png", "true", ""},
		{"shots/Home.JPEG", "true", ""},
		{"~/Desktop/Screenshot of the home page at 10.00.00.png", "true", ""},
		{"gone.png", "false", "missing"}, // only mentioned
		{"../notes.png", "false", "not_image"},
		{"../dir.png", "false", "not_image"},
		{filepath.Join(x.beyond, "secret.png"), "false", "outside"},
		{"../out.png", "false", "outside"},
		{"../big.png", "true", "too_big"}, // a picture: the sheet says it is too big
		{"a\ntouch x\n.png", "false", "path"},
	})
}

func TestImageCheckRefusals(t *testing.T) {
	x := imageFixture(t)
	if code, _ := x.check(t, "t404", "a.png"); code != http.StatusNotFound {
		t.Fatalf("an unknown terminal: %d", code)
	}
	many := make([]string, 65)
	for i := range many {
		many[i] = fmt.Sprintf("p%d.png", i)
	}
	if code, _ := x.check(t, "t1", many...); code != http.StatusBadRequest {
		t.Fatalf("65 paths: %d, want 400", code)
	}
	x.token = ""
	if code, _ := x.check(t, "t1", "a.png"); code != http.StatusUnauthorized {
		t.Fatalf("no paired device: %d", code)
	}
}

func TestImageCheckOnAnotherMachine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the check runs in sh on that machine")
	}
	x := remoteFixture(t)
	x.checked(t, "box/t3", [][3]string{
		{"../shot.png", "true", ""},
		{"up.png", "true", ""},
		{"~/Desktop/Screenshot of the home page at 10.00.00.png", "true", ""},
		{"gone.png", "false", "missing"},
		{"~/proj/notes.png", "false", "not_image"},
		{"~/proj/dir.png", "false", "not_image"},
		{"~/proj/out.png", "false", "outside"},
		{"~/proj/away/secret.png", "false", "outside"},
		{"~/proj/big.png", "true", "too_big"},
		{"a; touch /tmp/x #.png", "false", "missing"},
	})
	if code, _ := x.check(t, "gone/t1", "a.png"); code != http.StatusBadGateway {
		t.Fatalf("a machine that can't be reached: %d", code)
	}
}
