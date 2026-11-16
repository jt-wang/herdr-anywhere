package main

import (
	"herdr-anywhere/companion/internal/testfake"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmptyInputScreenJSON is hc read --json for a pane showing an empty Claude Code input box.
const fakeEmptyInputScreenJSON = `{"complete":true,"how":"screen","text":"──────────────────────────\n❯ \n──────────────────────────\n  ⏵⏵ auto mode on (shift+tab to cycle)\n"}`

// fakeHCFiles answers ls from ls.json, get from get-<t>.json and read from read-<t>.json;
// a pane starts with an empty input box, and sends and keys are logged.
func fakeHCFiles(t *testing.T, ls string, gets map[string]string) (hc, sent string) {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "ls.json"), []byte(ls), 0o600)
	for addr, g := range gets {
		os.WriteFile(filepath.Join(d, "get-"+addr+".json"), []byte(g), 0o600)
		os.WriteFile(filepath.Join(d, "read-"+addr+".json"), []byte(fakeEmptyInputScreenJSON), 0o600)
	}
	sent = filepath.Join(d, "sent.log")
	hc = testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", On: map[string][]testfake.Reply{
		"ls":   {{Cat: filepath.Join(d, "ls.json")}},
		"get":  {{Cat: filepath.Join(d, "get-{2}.json")}},
		"read": {{Cat: filepath.Join(d, "read-{2}.json")}},
		"send": {{Append: sent, Print: `{"sent":true}`}},
		"keys": {{Append: sent, Print: `{"sent":true}`}},
	}})
	return hc, sent
}

const p8List = `{"terminals":[
{"address":"t1h","name":"api","state":"live","status":"done","turn":1,"agent":"claude-code"},
{"address":"t1j","name":"web","state":"live","status":"blocked","turn":1,"agent":"claude-code"},
{"address":"t1k","name":"launch","state":"live","turn":0,"agent":"claude-code"}]}`

var p8Gets = map[string]string{
	"t1h": `{"address":"t1h","name":"api","state":"live","status":{"state":"done"},"turn":1}`,
	"t1j": `{"address":"t1j","name":"web","state":"live","status":{"kind":"permission","msg":"approve Bash: cat README.md","state":"blocked"},"turn":1}`,
	"t1k": `{"address":"t1k","name":"launch","state":"live","status":null,"turn":0}`,
}

func init() { // hc get takes a name as well as an address
	for addr, name := range map[string]string{"t1h": "api", "t1j": "web", "t1k": "launch"} {
		p8Gets[name] = p8Gets[addr]
	}
}

// The tree says what a blocked pane is asking, so the phone can show it.
func TestTreeShowsWhatABlockedPaneAsks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.json")
	hc, _ := fakeHCFiles(t, p8List, p8Gets)
	common := []string{"--file", file, "--hc", hc, "--herdr", "-"}
	runTrack(t, append([]string{"add"}, append(common, "T2", "官网深色模式")...)...)
	handedTo(t, common, "T2", "hc:web", "加深色模式")
	out, _, _ := runTrack(t, append([]string{"tree", "--json"}, common...)...)
	if !strings.Contains(out, "approve Bash: cat README.md") {
		t.Fatalf("tree:\n%s", out)
	}
}
