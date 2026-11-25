package cursor

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSavedTitle(t *testing.T) {
	r, p, d := fixture(t, "store.db")
	var raw string
	d.QueryRow("SELECT value FROM meta WHERE key='0'").Scan(&raw)
	b, _ := hex.DecodeString(raw)
	var m map[string]any
	json.Unmarshal(b, &m)
	want := m["name"].(string)
	got, e := r.Title(p)
	if e != nil || got != want || got == "" {
		t.Fatalf("title_present=%t err=%v", got != "", e)
	}
	for _, id := range []string{"wrong-id", fixtureID} {
		m["agentId"] = id
		m["name"] = "Rename garden"
		b, _ = json.Marshal(m)
		d.Exec("UPDATE meta SET value=? WHERE key='0'", hex.EncodeToString(b))
		got, _ = r.Title(p)
		if (id == fixtureID) != (got == "Rename garden") {
			t.Fatal("wrong metadata identity")
		}
	}
	m["name"] = strings.Repeat("x", 300000)
	b, _ = json.Marshal(m)
	d.Exec("UPDATE meta SET value=? WHERE key='0'", hex.EncodeToString(b))
	if got, _ = r.Title(p); got != "" {
		t.Fatal("unbounded metadata")
	}
}

func TestSavedTitleCheckpointed(t *testing.T) {
	r, p, d := fixture(t, "store.db")
	if _, e := d.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		t.Fatal(e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	for _, f := range []string{p + "-wal", p + "-shm"} {
		if _, e := os.Stat(f); !os.IsNotExist(e) {
			t.Fatal("fixture sidecar remains")
		}
	}
	got, e := r.Title(p)
	if e != nil || got == "" {
		t.Fatalf("checkpointed title_present=%t err=%v", got != "", e)
	}
	for _, f := range []string{p + "-wal", p + "-shm"} {
		if _, e := os.Stat(f); !os.IsNotExist(e) {
			t.Fatal("created sidecar")
		}
	}
}
