package pi

import (
	"errors"
	"os"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func TestLeafSnapshotExpiresAfterRuntimeBoundary(t *testing.T) {
	for _, active := range []string{"", `,"activeLeafId":"a2"`, `,"activeLeafId":null`} {
		t.Run(active, func(t *testing.T) {
			r, p, file := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "FILE"})
			r.Setup.ID = "omp"
			if err := r.Setup.Install(false); err != nil {
				t.Fatal(err)
			}
			c := forked()
			c.ls = append(c.ls, `{"type":"custom","customType":"session_exit","id":"exit1","parentId":"a1","data":{"reason":"dispose","kind":"normal","recordedAt":"2026-01-01T12:00:01Z"}}`)
			contents, err := os.ReadFile(c.write(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			p.Labels[leafLabel] = "exit1:exit1"
			path, _, err := r.FindPane(p)
			if err != nil || path == file {
				t.Fatalf("initial snapshot: %q %v", path, err)
			}
			wantTurns(t, r, path, "u1")
			appendRuntimeRecord(t, file, `{"type":"custom","customType":"session_exit","id":"exit2","parentId":"a2"`+active+`,"data":{"reason":"dispose","kind":"normal","recordedAt":"2026-01-01T12:00:02Z"}}`+"\n")
			path, _, err = r.FindPane(p)
			if err != nil || path != file {
				t.Fatalf("stale snapshot retained: %q %v", path, err)
			}
			if active == `,"activeLeafId":null` {
				wantTurns(t, r, path)
			} else {
				wantTurns(t, r, path, "u1", "u2")
			}
			for i := 0; i < 20; i++ {
				p.Labels[leafLabel] = "a1:exit2"
				if i%2 == 1 {
					p.Labels[leafLabel] = "a2:exit2"
				}
				path, _, err = r.FindPane(p)
				if err != nil || path == file {
					t.Fatalf("fresh runtime snapshot rejected: %q %v", path, err)
				}
				if i%2 == 0 {
					wantTurns(t, r, path, "u1")
				} else {
					wantTurns(t, r, path, "u1", "u2")
				}
			}
			if len(r.leaves) != 1 || len(r.cache) != 1 {
				t.Fatalf("unbounded caches: %d leaf, %d parsed", len(r.leaves), len(r.cache))
			}
		})
	}
}

func TestLeafRuntimeBoundaryAcrossPartialAppend(t *testing.T) {
	file := forked().write(t)
	r := &Reader{}
	check := func(want bool) {
		t.Helper()
		st, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		expired, err := r.expiredLeaf(file, "a1:a2", st)
		if err != nil || expired != want {
			t.Fatalf("expired %v, want %v: %v", expired, want, err)
		}
	}
	check(false)
	appendRuntimeRecord(t, file, `{"type":"custom","customType":"tool_execution_start","id":"tool","parentId":"a2"}`+"\n")
	check(false)
	appendRuntimeRecord(t, file, "\n"+`{"type":"custom","customType":"session_`)
	check(false)
	appendRuntimeRecord(t, file, `exit","id":"exit","parentId":"a2"}`+"\n")
	check(true)
	check(true)
	appendRuntimeRecord(t, file, `{"type":"message","id":"later","parentId":"a2","message":{"role":"user","content":"later"}}`+"\n")
	check(true)
}

func TestLeafRuntimeBoundaryRejectsMalformedJournal(t *testing.T) {
	file := forked().write(t)
	appendRuntimeRecord(t, file, "{broken}\n")
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	var format *chat.FormatError
	if _, err := (&Reader{}).expiredLeaf(file, "a1:a2", st); !errors.As(err, &format) {
		t.Fatalf("invalid journal: %v", err)
	}
}

func appendRuntimeRecord(t *testing.T, file, record string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(record); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
