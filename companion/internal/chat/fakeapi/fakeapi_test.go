package fakeapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// ROWS answers with a list long enough to scroll: 150 rows, each with its number, so a test can tell how far it moved.
func TestRowsIsALongNumberedList(t *testing.T) {
	msgs := []json.RawMessage{json.RawMessage(`{"role":"user","content":[{"type":"text","text":"ROWS please"}]}`)}
	st := (&Server{}).answer(msgs, "", true)
	if len(st.Blocks) != 1 || st.Stop != "end_turn" {
		t.Fatalf("got %v", st)
	}
	got, _ := st.Blocks[0]["text"].(string)
	for _, want := range []string{"- acme-api line 1\n", "- acme-api line 75\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	if !strings.HasSuffix(got, "- acme-api line 150") {
		t.Errorf("does not end with row 150: %q", got[max(0, len(got)-40):])
	}
	if n := strings.Count(got, "- acme-api line "); n != 150 {
		t.Errorf("%d rows, want 150", n)
	}
}
