package main

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/makiuchi-d/gozxing"
	gozxingqr "github.com/makiuchi-d/gozxing/qrcode"
	tc "github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"

	"herdr-anywhere/companion/internal/tailcat"
)

// fakeInvite is a herdr1 invite as long as a real one: made-up keys and a made-up relay region of two nodes
// (documentation addresses), which is what makes the real ones about 600 characters.
func fakeInvite(t *testing.T) string {
	t.Helper()
	node := func(host, v4, v6 string) *tailcfg.DERPNode {
		return &tailcfg.DERPNode{Name: host, RegionID: 900, HostName: host, IPv4: v4, IPv6: v6}
	}
	region := &tailcfg.DERPRegion{RegionID: 900, RegionCode: "tst", RegionName: "Test", Nodes: []*tailcfg.DERPNode{
		node("relay-a.example.invalid", "192.0.2.10", "2001:db8::10"),
		node("relay-b.example.invalid", "192.0.2.11", "2001:db8::11"),
	}}
	ci := tc.ConnInfo{ServerPublic: tc.NodePublic{NodePublic: key.NewNode().Public()}, ServerDiscoPublic: tc.DiscoPublic{DiscoPublic: key.NewDisco().Public()},
		PresharedKey: tc.NewPresharedKey(), Region: []*tailcfg.DERPRegion{region}}
	client, _ := key.NewNode().MarshalText()
	text, err := connectinvite.Encode(connectinvite.Invite{Version: connectinvite.Version, Address: string(ci.Addr()), Port: 41641,
		Key: string(client), Proof: connectinvite.NewProof(), ExpiresAt: time.Now().Add(2 * time.Minute).Unix(), Name: "Test Mac"}, tailcat.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) < 550 {
		t.Fatalf("fake invite is %d characters; real ones are about 600", len(text))
	}
	return text
}

// asTerminal makes pair see a terminal of cols × rows, and records the images it opens; open's error is what opening gives.
func asTerminal(t *testing.T, cols, rows int, open error) *[]string {
	t.Helper()
	opened := new([]string)
	oldSize, oldOpen := terminalSize, openImage
	terminalSize = func(any) (int, int, bool) { return cols, rows, true }
	openImage = func(path string) error { *opened = append(*opened, path); return open }
	t.Cleanup(func() { terminalSize, openImage = oldSize, oldOpen })
	return opened
}

// qrLines are the lines of a terminal QR code in out: the half-block rows.
func qrLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.ContainsAny(l, "▀▄█") {
			lines = append(lines, l)
		}
	}
	return lines
}

func decodePNG(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	// PURE_BARCODE reads the modules straight off this clean, unrotated image. Without it, gozxing's finder-pattern
	// search misses about 1 in 40 of these 81-module codes (NotFoundException, FormatException) though the image is
	// right: macOS CoreImage, and gozxing with this hint, read every one of them.
	res, err := gozxingqr.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_PURE_BARCODE: true})
	if err != nil {
		t.Fatalf("%s does not decode as a QR code: %v", path, err)
	}
	return res.GetText()
}

// The terminal QR code is as big as qrFootprint says, so pair can tell whether it fits before printing it.
func TestQRFootprintIsWhatPrintQRDraws(t *testing.T) {
	for _, text := range []string{"http://192.0.2.5:7681/#pair=abcdefghijklmnopqrstuv", fakeInvite(t)} {
		var b bytes.Buffer
		printQR(&b, text)
		lines := qrLines(b.String())
		cols, rows := qrFootprint(text)
		width := 0
		for _, l := range lines {
			width = max(width, utf8.RuneCountInString(l))
		}
		if len(lines) != rows || width != cols {
			t.Fatalf("%d-character text: qrFootprint says %d×%d, printQR drew %d×%d", len(text), cols, rows, width, len(lines))
		}
	}
}

// A laptop terminal of 120×35 cannot show a 600-character invite's code, which is 89×45. pair opens it as an image
// instead, and the image reads back as the invite.
func TestPairOpensTheQRAsAnImageWhenTheTerminalIsTooSmall(t *testing.T) {
	dir := shortStateDir(t)
	invite := fakeInvite(t)
	fakeTailcatServeInvite(t, dir, []string{"http://127.0.0.1:7681"}, invite)
	opened := asTerminal(t, 120, 35, nil)
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	if l := qrLines(out); len(l) > 0 {
		t.Fatalf("printed a %d-row QR code into a 35-row terminal:\n%s", len(l), out)
	}
	want := filepath.Join(dir, pairImageFile)
	if !slices.Equal(*opened, []string{want}) {
		t.Fatalf("opened %v, want %s", *opened, want)
	}
	if !strings.Contains(out, want) || !strings.Contains(out, invite) {
		t.Fatalf("output names neither the image nor the invite:\n%s", out)
	}
	if got := decodePNG(t, want); got != invite {
		t.Fatalf("the image reads %q, want the invite", got)
	}
	// Windows has no mode bits (Go reports every file as rw-rw-rw-); there the access list of the user's home folder, where the state folder is, keeps it.
	if fi, err := os.Stat(want); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0) {
		t.Fatalf("the image holds a pairing secret; mode %v, %v", fi.Mode(), err)
	}
}

// Over ssh, or on Linux with no display, there is no screen to open the image on: pair says where it is and how
// else to pair, and does not fail.
func TestPairWithNoScreenSaysWhereTheImageIs(t *testing.T) {
	dir := shortStateDir(t)
	invite := fakeInvite(t)
	fakeTailcatServeInvite(t, dir, []string{"http://127.0.0.1:7681"}, invite)
	asTerminal(t, 100, 30, errNoScreen)
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	want := filepath.Join(dir, pairImageFile)
	if !strings.Contains(out, want) || !strings.Contains(out, invite) {
		t.Fatalf("output names neither the image nor the invite:\n%s", out)
	}
	cols, rows := qrFootprint(invite)
	if !regexp.MustCompile(`\b` + strconv.Itoa(cols) + `\D+` + strconv.Itoa(rows+2) + `\b`).MatchString(out) {
		t.Fatalf("output doesn't say how big the terminal has to be (%d×%d):\n%s", cols, rows+2, out)
	}
	if got := decodePNG(t, want); got != invite {
		t.Fatalf("the image reads %q, want the invite", got)
	}
}

// In a terminal it fits, the code is printed last, so nothing after it pushes its top off the screen, and no image is made.
func TestPairPrintsTheQRLastWhenItFits(t *testing.T) {
	dir := shortStateDir(t)
	invite := fakeInvite(t)
	fakeTailcatServeInvite(t, dir, []string{"http://127.0.0.1:7681", "http://192.0.2.20:7681"}, invite)
	cols, rows := qrFootprint(invite)
	opened := asTerminal(t, cols, rows+2, nil)
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	if len(*opened) > 0 {
		t.Fatalf("opened %v though the code fits", *opened)
	}
	if _, err := os.Stat(filepath.Join(dir, pairImageFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote an image though the code fits: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var after int
	for i := len(lines) - 1; i >= 0 && !strings.ContainsAny(lines[i], "▀▄█"); i-- {
		after++
	}
	if len(qrLines(out)) != rows || after > 1 {
		t.Fatalf("want the %d-row code with at most one line after it, got %d rows and %d lines after:\n%s", rows, len(qrLines(out)), after, out)
	}
	if !strings.Contains(out, invite) || !strings.Contains(out, "http://192.0.2.20:7681/#pair=") {
		t.Fatalf("invite or LAN link missing:\n%s", out)
	}
}

// An image left by an earlier pair, too big for its terminal then, goes when the next one fits.
func TestPairRemovesAnOldImage(t *testing.T) {
	dir := shortStateDir(t)
	invite := fakeInvite(t)
	fakeTailcatServeInvite(t, dir, []string{"http://127.0.0.1:7681"}, invite)
	asTerminal(t, 120, 35, nil)
	if _, errs, code := runCLI(t, "pair", "--state", dir); code != 0 {
		t.Fatal(errs)
	}
	asTerminal(t, 200, 80, nil)
	if _, errs, code := runCLI(t, "pair", "--state", dir); code != 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(filepath.Join(dir, pairImageFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old image, which holds a used invite, is still there: %v", err)
	}
}

// How each system shows an image, and when there is no screen to show it on.
func TestImageOpener(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			if slices.Contains(names, name) {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	const p = "/state/pair-qr.png"
	cases := []struct {
		name string
		goos string
		env  func(string) string
		path func(string) (string, error)
		want []string // nil: errNoScreen
	}{
		{"macOS", "darwin", env(), has("open"), []string{"open", p}},
		{"macOS over ssh", "darwin", env("SSH_CONNECTION", "192.0.2.1 50000 192.0.2.2 22"), has("open"), nil},
		{"Windows", "windows", env(), has(), []string{"rundll32.exe", "url.dll,FileProtocolHandler", p}},
		{"Windows over ssh", "windows", env("SSH_CLIENT", "192.0.2.1 50000 22"), has(), nil},
		{"Linux desktop", "linux", env("DISPLAY", ":0"), has("xdg-open"), []string{"xdg-open", p}},
		{"Linux Wayland", "linux", env("WAYLAND_DISPLAY", "wayland-0"), has("xdg-open"), []string{"xdg-open", p}},
		{"Linux with no display", "linux", env(), has("xdg-open"), nil},
		{"Linux with no xdg-open", "linux", env("DISPLAY", ":0"), has(), nil},
		// X forwarding shows it on the screen the person sits at.
		{"Linux over ssh with X forwarding", "linux", env("DISPLAY", "localhost:10.0", "SSH_TTY", "/dev/pts/1"), has("xdg-open"), []string{"xdg-open", p}},
		{"Linux over ssh", "linux", env("SSH_TTY", "/dev/pts/1"), has("xdg-open"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := imageOpener(c.goos, c.env, c.path, p)
			if c.want == nil {
				if !errors.Is(err, errNoScreen) {
					t.Fatalf("got %v, %v; want errNoScreen", got, err)
				}
				return
			}
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatalf("got %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

// The image holds the invite, and with it the computer's Tailcat address, a secret beyond the code's few minutes:
// serve removes it when the pairing window it shows closes, but not one a later pair made for a later window.
func TestServeRemovesTheImageWhenItsWindowCloses(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, pairImageFile)
	length := 150 * time.Millisecond
	open := removeImageAfter(dir, func(context.Context, string) (tailcat.Window, error) {
		return tailcat.Window{Invite: "herdr1:x", Expires: time.Now().Add(length)}, nil
	})
	if _, err := open(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(img, []byte("first"), 0o600)
	waitFor(t, 5*time.Second, func() bool { _, err := os.Stat(img); return errors.Is(err, os.ErrNotExist) }, "the image to go with its window", &bytes.Buffer{})

	open(context.Background(), "b")
	os.WriteFile(img, []byte("second"), 0o600)
	time.Sleep(length / 2)
	length = time.Minute
	open(context.Background(), "c") // a later pair, whose window is still open when b's closes
	os.WriteFile(img, []byte("third"), 0o600)
	time.Sleep(300 * time.Millisecond) // b's window closed 225 ms ago
	if b, err := os.ReadFile(img); err != nil || string(b) != "third" {
		t.Fatalf("an earlier window's end removed a later pair's image: %q %v", b, err)
	}
}
