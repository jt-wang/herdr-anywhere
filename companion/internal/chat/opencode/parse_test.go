package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// fixture: the rows of one session OpenCode wrote (testdata/opencode/<version>/<name>.json, exported from its
// database after a run against the fake Responses API).
type fixture struct {
	Sessions []struct {
		ID, Directory, Title, Version string
	} `json:"sessions"`
	Messages []message `json:"messages"`
}

// fixture1: the rows of one session OpenCode 1 wrote: its session, its messages, and their parts.
type fixture1 struct {
	Sessions []sess1 `json:"sessions"`
	Messages []row1  `json:"messages"`
	Parts    []row1  `json:"parts"`
}

type sess1 struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Version   string `json:"version"`
	Created   int64  `json:"time_created"`
	Updated   int64  `json:"time_updated"`
}

// row1: a row of OpenCode 1's message or part table.
type row1 struct {
	ID      string          `json:"id"`
	Message string          `json:"message_id,omitempty"`
	Session string          `json:"session_id"`
	Created int64           `json:"time_created"`
	Updated int64           `json:"time_updated"`
	Data    json.RawMessage `json:"data"`
}

// fromFixture: the turns of a fixture of either version.
func fromFixture(t *testing.T, b []byte) []chat.Turn {
	t.Helper()
	var probe struct {
		Parts json.RawMessage `json:"parts"`
	}
	json.Unmarshal(b, &probe)
	if probe.Parts != nil {
		var f fixture1
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatal(err)
		}
		var ms []msg1
		var ps []part1
		for _, m := range f.Messages {
			ms = append(ms, msg1{ID: m.ID, Created: m.Created, Data: m.Data})
		}
		for _, p := range f.Parts {
			ps = append(ps, part1{ID: p.ID, Message: p.Message, Data: p.Data})
		}
		return parse(convert1(ms, ps))
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return parse(f.Messages)
}

func load(t *testing.T, path string) fixture {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return f
}

func norm(ts []chat.Turn) []chat.Turn {
	out := []chat.Turn{}
	for _, t := range ts {
		t.ID, t.Secs, t.At = "", 0, ""
		steps := []chat.Step{}
		for _, s := range t.Steps {
			s.ID = ""
			steps = append(steps, s)
		}
		t.Steps = steps
		out = append(out, t)
	}
	return out
}

// Every session OpenCode wrote reads as its .want.json says.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "opencode", "*", "*.json"))
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".want.json") {
			continue
		}
		n++
		t.Run(f, func(t *testing.T) {
			b, err := os.ReadFile(strings.TrimSuffix(f, ".json") + ".want.json")
			if err != nil {
				t.Fatal(err)
			}
			var want []chat.Turn
			json.Unmarshal(b, &want)
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got := fromFixture(t, raw)
			if !reflect.DeepEqual(norm(got), norm(want)) {
				g, _ := json.MarshalIndent(norm(got), "", "  ")
				t.Fatalf("got\n%s", g)
			}
		})
	}
	if n == 0 {
		t.Fatal("no fixtures")
	}
}
