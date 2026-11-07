//go:build windows

package server

import (
	"path/filepath"
	"testing"
)

// What the phone's folder picker gets from a Windows computer (app/src/model/folders.ts reads these shapes):
// `\` between parts, `/` typed read as `\`, `~\` as well as `~/` for home, and a drive's folder above is the drive.
func TestFoldersOnWindows(t *testing.T) {
	f, home, token := folderFixture(t)
	src := filepath.Join(home, "src")
	for _, p := range []string{`~\src`, "~/src", filepath.ToSlash(src)} {
		code, body := folders(t, f, token, p)
		if code != 200 || body["path"] != src || body["parent"] != home {
			t.Fatalf("%q: status %d, path %v, parent %v; want %s in %s", p, code, body["path"], body["parent"], src, home)
		}
	}
	drive := filepath.VolumeName(home) + `\`
	code, body := folders(t, f, token, drive)
	if code != 200 || body["path"] != drive || body["parent"] != drive {
		t.Fatalf("%q: status %d, path %v, parent %v", drive, code, body["path"], body["parent"])
	}
	_, body = folders(t, f, token, "")
	t.Logf("home: path %q, home %q, parent %q", body["path"], body["home"], body["parent"])
}
