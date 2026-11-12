package fixtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// files: every recorded session under the readers' testdata.
func files(t *testing.T) []string {
	var out []string
	for _, agent := range []string{"claude", "codex", "pi", "opencode"} {
		filepath.Walk(filepath.Join("..", agent, "testdata"), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && (strings.HasSuffix(p, ".jsonl") || strings.HasSuffix(p, ".json")) && !strings.HasSuffix(p, ".want.json") {
				out = append(out, p)
			}
			return nil
		})
	}
	if len(out) == 0 {
		t.Fatal("no testdata found")
	}
	return out
}

// A published fixture tells nothing of when it was recorded: each starts on the made-up day.
func TestFixturesStartOnTheMadeUpDay(t *testing.T) {
	for _, p := range files(t) {
		b, _ := os.ReadFile(p)
		first, ok := Earliest(b)
		if !ok {
			continue
		}
		if first.Before(Day) || first.Sub(Day) >= time.Second {
			t.Errorf("%s starts at %s, not on %s", p, first.Format(time.RFC3339Nano), Day.Format(time.RFC3339))
		}
		if last := Latest(b); last.Sub(Day) > 24*time.Hour {
			t.Errorf("%s runs to %s, past the made-up day", p, last.Format(time.RFC3339Nano))
		}
	}
}

// Each way a session carries its time moves by the same amount; nothing else changes. Made-up times; the expected
// values worked out apart from this code.
func TestByMovesEveryKindOfTime(t *testing.T) {
	d := -10 * 24 * time.Hour
	in := `{"timestamp":"2026-03-14T09:26:53.589Z","file":"rollout-2026-03-14T09-26-53-x","ms":1773480413589,"s":[1773480413,1773480414],` +
		`"id":"019cebab-8907-74c0-a44e-f9b663f22657","msg":"msg_cebab8995001WGyFI8fRykpF0q","ses":"ses_3145476a4ffe1Oe1UHic1xuAFb",` +
		`"tokens":1234567890,"size":17000000000000000,"v":"1.773480413","ct":1773480413.326993,"m":"msg_019cebab-896d-7b73-b64e-52eb7c11bec3"}`
	want := `{"timestamp":"2026-03-04T09:26:53.589Z","file":"rollout-2026-03-04T09-26-53-x","ms":1772616413589,"s":[1772616413,1772616414],` +
		`"id":"019cb82b-f107-74c0-a44e-f9b663f22657","msg":"msg_cb82bf195001WGyFI8fRykpF0q","ses":"ses_347d40ea4ffe1Oe1UHic1xuAFb",` +
		`"tokens":1234567890,"size":17000000000000000,"v":"1.773480413","ct":1772616413.326993,"m":"msg_019cb82b-f16d-7b73-b64e-52eb7c11bec3"}`
	if got := string(By([]byte(in), d)); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// Order among OpenCode's ids stays: newer messages sort after.
	if f := strings.Fields(string(By([]byte("msg_cebab8995001AAAAAAAAAAAAAA msg_cebab89a5001AAAAAAAAAAAAAA"), d))); !(f[0] < f[1]) {
		t.Fatalf("message order changed: %v", f)
	}
}

// OpenCode's ids hold (ms mod 2^36)<<12 | a counter, as By takes them: in OpenCode 2.2.0's own rows (testdata, on the
// made-up day) each message's id holds a time a few ms before the row's own.
func TestOpenCodeIdsHoldTheirTime(t *testing.T) {
	b, err := os.ReadFile("../opencode/testdata/opencode/2.2.0/turns.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Messages []struct {
			ID      string `json:"id"`
			Created int64  `json:"time_created"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Messages) == 0 {
		t.Fatalf("%v, %d messages", err, len(f.Messages))
	}
	for _, m := range f.Messages {
		var v uint64
		fmt.Sscanf(m.ID[4:16], "%x", &v)
		if gap := m.Created%(1<<36) - int64(v>>12); gap < 0 || gap > 1000 {
			t.Errorf("%s at %d: the id says %d ms mod 2^36, %d ms off", m.ID, m.Created, v>>12, gap)
		}
	}
}

func TestShiftPutsTheFirstTimeOnTheDay(t *testing.T) {
	out, d := Shift([]byte(`{"a":"2026-03-14T09:26:53.589Z","b":"2026-03-14T09:27:50.000Z","c":1773480413}`))
	if d%time.Second != 0 {
		t.Fatalf("shift %v is not whole seconds", d)
	}
	if string(out) != `{"a":"2026-01-01T12:00:00.589Z","b":"2026-01-01T12:00:57.000Z","c":1767268800}` {
		t.Fatalf("got %s", out)
	}
	if _, d := Shift([]byte(`{"n":42}`)); d != 0 {
		t.Fatalf("no time, yet shifted by %v", d)
	}
}

// FIXTIME_REWRITE=1 moves the readers' testdata onto Day in place: each recorded session by its own first time, its
// .want.json files by the same amount.
func TestRewrite(t *testing.T) {
	if os.Getenv("FIXTIME_REWRITE") == "" {
		t.Skip("FIXTIME_REWRITE=1 rewrites the testdata")
	}
	for _, p := range files(t) {
		b, _ := os.ReadFile(p)
		out, d := Shift(b)
		if d == 0 {
			continue
		}
		os.WriteFile(p, out, 0o644)
		base := strings.TrimSuffix(strings.TrimSuffix(p, ".jsonl"), ".json")
		wants, _ := filepath.Glob(base + ".*want.json")
		for _, w := range wants {
			wb, _ := os.ReadFile(w)
			os.WriteFile(w, By(wb, d), 0o644)
		}
		t.Logf("%s and %d want files: moved by %v", p, len(wants), d)
	}
}
