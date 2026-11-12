package opencode

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// db makes an opencode.db with OpenCode 2.2.0's tables (testdata/.../schema.sql) holding the fixtures' rows, the way
// a running OpenCode keeps it: in WAL mode, open until the test ends (its -wal and -shm files are there).
func db(t *testing.T, names ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "opencode.db")
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	schema, err := os.ReadFile(filepath.Join("testdata", "opencode", "2.2.0", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("PRAGMA journal_mode=WAL;" + string(schema)); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		f := load(t, filepath.Join("testdata", "opencode", "2.2.0", n+".json"))
		for _, s := range f.Sessions {
			add(t, d, s.ID, s.Directory, s.Title, s.Version, "")
		}
		for _, m := range f.Messages {
			insert(t, d, f.Sessions[0].ID, m)
		}
	}
	return p
}

func add(t *testing.T, d *sql.DB, id, dir, title, version, parent string) {
	t.Helper()
	var par any
	if parent != "" {
		par = parent
	}
	if _, err := d.Exec("INSERT INTO session_v2 (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES (?, 'p', ?, 's', ?, ?, ?, 1, 1)",
		id, par, dir, title, version); err != nil {
		t.Fatal(err)
	}
}

func insert(t *testing.T, d *sql.DB, session string, m message) {
	t.Helper()
	if _, err := d.Exec("INSERT INTO session_message (id, session_id, type, seq, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?, ?)",
		m.ID, session, m.Type, m.Seq, m.Created, m.Created, string(m.Data)); err != nil {
		t.Fatal(err)
	}
}

func session(t *testing.T, name string) (id, title string) {
	f := load(t, filepath.Join("testdata", "opencode", "2.2.0", name+".json"))
	return f.Sessions[0].ID, f.Sessions[0].Title
}

func reason(err error) string {
	var m *chat.Miss
	if errors.As(err, &m) {
		return m.Reason
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// The session OpenCode reported is the one shown, with its version.
func TestFindPaneByReportedSession(t *testing.T) {
	p := db(t, "turns", "steps")
	id, _ := session(t, "steps")
	r := &Reader{DB: p}
	path, v, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{id}})
	if err != nil || path != p+"#"+id || v != "2.2.0" {
		t.Fatalf("got %q %q %v", path, v, err)
	}
	// One it reported that isn't in the database: not this database's OpenCode.
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{"ses_other"}}); reason(err) != chat.NoMatch {
		t.Fatalf("unknown id: %v", err)
	}
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{id, "ses_other"}}); reason(err) != chat.Ambiguous {
		t.Fatalf("two ids: %v", err)
	}
}

// Nothing reported: the terminal title OpenCode writes (`OC | ` and the title, cut past 40 UTF-16 units) names the
// session among those in its folder; anything else waits for its next report.
func TestFindPaneByTitle(t *testing.T) {
	p := db(t, "turns", "steps")
	id, title := session(t, "turns")
	r := &Reader{DB: p}
	path, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Title: "OC | " + title})
	if err != nil || path != p+"#"+id {
		t.Fatalf("title: %q %v", path, err)
	}
	for name, pane := range map[string]chat.Pane{
		"other folder":   {Cwd: "/work/other", Title: "OC | " + title},
		"home screen":    {Cwd: "/work/acme", Title: "OpenCode"},
		"no title":       {Cwd: "/work/acme"},
		"another title":  {Cwd: "/work/acme", Title: "OC | Something else"},
		"title, no OC |": {Cwd: "/work/acme", Title: title},
	} {
		if _, _, err := r.FindPane(pane); reason(err) != chat.NotReported {
			t.Errorf("%s: %v", name, err)
		}
	}

	d, _ := sql.Open("sqlite", p)
	defer d.Close()
	long := "Make the login page buttons round on every screen" // 50 units
	add(t, d, "ses_long", "/work/acme", long, "2.2.0", "")
	add(t, d, "ses_child", "/work/acme", "Child task", "2.2.0", "ses_long")
	add(t, d, "ses_twin1", "/work/acme", "Same name", "2.2.0", "")
	add(t, d, "ses_twin2", "/work/acme", "Same name", "2.2.0", "")
	add(t, d, "ses_new", "/work/acme", "New session - 2026-01-01T18:35:23.456Z", "2.2.0", "")
	if path, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Title: "OC | " + long[:37] + "…"}); err != nil || path != p+"#ses_long" {
		t.Fatalf("long title: %q %v", path, err)
	}
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Title: "OC | " + long}); reason(err) != chat.NotReported {
		t.Fatalf("uncut long title matched: %v", err)
	}
	for name, title := range map[string]string{"helper": "OC | Child task", "twins": "OC | Same name", "untitled": "OC | New session - 2026-01-01T18:35:23.456Z"} {
		if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Title: title}); reason(err) != chat.NotReported {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// OpenCode 1's database (testdata/opencode/1.19.0/schema.sql, from 1.19.0's own): sessions in `session`, their
// messages in `message` and `part`, `session_message` empty; holding the 1.19.0 fixtures' rows, open in WAL mode.
func db1(t *testing.T, names ...string) (string, *sql.DB) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "opencode.db")
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	schema, err := os.ReadFile(filepath.Join("testdata", "opencode", "1.19.0", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("PRAGMA journal_mode=WAL;" + string(schema)); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		f := load1(t, n)
		for _, x := range f.Sessions {
			if _, err := d.Exec("INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated) VALUES (?, 'p', 's', ?, ?, ?, ?, ?)",
				x.ID, x.Directory, x.Title, x.Version, x.Created, x.Updated); err != nil {
				t.Fatal(err)
			}
		}
		for _, m := range f.Messages {
			if _, err := d.Exec("INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)", m.ID, m.Session, m.Created, m.Updated, string(m.Data)); err != nil {
				t.Fatal(err)
			}
		}
		for _, x := range f.Parts {
			if _, err := d.Exec("INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)",
				x.ID, x.Message, x.Session, x.Created, x.Updated, string(x.Data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return p, d
}

func load1(t *testing.T, name string) fixture1 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "opencode", "1.19.0", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture1
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// OpenCode 1 reports no session; its terminal title (the same rule as 2's) names it among its folder's sessions,
// and its turns read as the fixtures 1.19.0 wrote say.
func TestOpenCode1(t *testing.T) {
	p, _ := db1(t, "turns", "steps", "files")
	r := &Reader{DB: p}
	for _, n := range []string{"turns", "files"} {
		s := load1(t, n).Sessions[0]
		path, v, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Title: title(s.Title)})
		if err != nil || path != p+"#"+s.ID || v != "1.19.0" {
			t.Fatalf("%s by title: %q %q %v", n, path, v, err)
		}
		got, err := r.Turns(path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join("testdata", "opencode", "1.19.0", n+".want.json"))
		var want []chat.Turn
		json.Unmarshal(b, &want)
		if !reflect.DeepEqual(norm(got), norm(want)) {
			g, _ := json.MarshalIndent(norm(got), "", "  ")
			t.Fatalf("%s: got\n%s", n, g)
		}
	}
	// Its id, if a pane ever reports one, finds it too.
	s := load1(t, "steps").Sessions[0]
	if path, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{s.ID}}); err != nil || path != p+"#"+s.ID {
		t.Fatalf("reported: %q %v", path, err)
	}
	// Untitled ("New session - …", shown as OpenCode), its home screen, another folder: not yet.
	for name, pane := range map[string]chat.Pane{
		"home screen": {Cwd: "/work/acme", Title: "OpenCode"},
		"other place": {Cwd: "/work/other", Title: title(load1(t, "turns").Sessions[0].Title)},
		"no title":    {Cwd: "/work/acme"},
	} {
		if _, _, err := r.FindPane(pane); reason(err) != chat.NotReported {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// OpenCode 1's step details: a command with what it printed and its exit code, a file's change, an image read.
func TestOpenCode1Detail(t *testing.T) {
	p, d := db1(t, "steps")
	s := load1(t, "steps").Sessions[0]
	path := p + "#" + s.ID
	r := &Reader{DB: p}
	got, err := r.Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join("testdata", "opencode", "1.19.0", "steps.want.json"))
	var want []chat.Turn
	json.Unmarshal(b, &want)
	if !reflect.DeepEqual(norm(got), norm(want)) {
		g, _ := json.MarshalIndent(norm(got), "", "  ")
		t.Fatalf("steps: got\n%s", g)
	}

	dt, err := r.Detail(path, stepOf(t, r, path, "run", "exit 3").ID)
	if err != nil || dt.Run == nil || !dt.Run.Failed || dt.Run.Code != 3 || dt.Run.Output != "checking\none test failed" || dt.Run.Lines != 2 {
		t.Fatalf("run: %+v %+v %v", dt, dt.Run, err)
	}
	dt, err = r.Detail(path, stepOf(t, r, path, "edit", "notes.txt").ID)
	if err != nil || dt.Change == nil {
		t.Fatalf("edit: %+v %v", dt, err)
	}
	wantC := chat.Change{File: "notes.txt", Added: 2, Hunks: []chat.Hunk{{OldStart: 1, NewStart: 1, Lines: []string{" a", "+b", "+c"}}}}
	if !reflect.DeepEqual(*dt.Change, wantC) {
		t.Fatalf("edit: %+v", *dt.Change)
	}
	dt, err = r.Detail(path, stepOf(t, r, path, "read", "shot.png").ID)
	if err != nil || dt.Input != "shot.png" || len(dt.Images) != 1 || dt.Images[0].Source != "shot.png" {
		t.Fatalf("read: %+v %v", dt, err)
	}
	media, img, err := r.Image(path, dt.Images[0].ID)
	if err != nil || media != "image/png" || !strings.HasPrefix(string(img), "\x89PNG") {
		t.Fatalf("image: %q %d %v", media, len(img), err)
	}

	// A new prompt OpenCode writes: read again; while it works the turn is open.
	before := len(got)
	if _, err := d.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES ('msg_more', ?, 1767268900000, 1767268900000, '{"role":"user","time":{"created":1767268900000}}')`, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_more', 'msg_more', ?, 1767268900000, 1767268900000, '{"type":"text","text":"ONE more thing"}')`, s.ID); err != nil {
		t.Fatal(err)
	}
	after, err := r.Turns(path)
	if err != nil || len(after) != before+1 || after[len(after)-1].Said != "ONE more thing" || after[len(after)-1].End != "open" {
		t.Fatalf("after a new prompt: %d turns, %v", len(after), err)
	}
}

// No OpenCode database on this computer: nothing to show yet, and no file made.
func TestFindPaneNoDatabase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.db")
	r := &Reader{DB: p}
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{"ses_x"}}); reason(err) != chat.NoMatch {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("made %s: %v", p, err)
	}
}

// Turns reads the session's rows as the fixtures say, and again after OpenCode adds one.
func TestTurnsFromDatabase(t *testing.T) {
	p := db(t, "turns", "steps", "files", "ask")
	r := &Reader{DB: p}
	for _, n := range []string{"turns", "steps", "files", "ask"} {
		id, _ := session(t, n)
		got, err := r.Turns(p + "#" + id)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join("testdata", "opencode", "2.2.0", n+".want.json"))
		var want []chat.Turn
		json.Unmarshal(b, &want)
		if !reflect.DeepEqual(norm(got), norm(want)) {
			t.Fatalf("%s: got %+v", n, got)
		}
	}
	id, _ := session(t, "turns")
	before, _ := r.Turns(p + "#" + id)
	d, _ := sql.Open("sqlite", p)
	defer d.Close()
	insert(t, d, id, message{ID: "msg_more", Type: "user", Seq: 1000, Created: 1767268800000, Data: json.RawMessage(`{"text":"ONE more thing"}`)})
	after, _ := r.Turns(p + "#" + id)
	if len(after) != len(before)+1 || after[len(after)-1].Said != "ONE more thing" {
		t.Fatalf("after a new row: %d turns, last %+v", len(after), after[len(after)-1])
	}
}

func stepOf(t *testing.T, r *Reader, path, kind, title string) chat.Step {
	t.Helper()
	ts, err := r.Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tu := range ts {
		for _, s := range tu.Steps {
			if s.Kind == kind && strings.Contains(s.Title, title) {
				return s
			}
		}
	}
	t.Fatalf("no %s step %q", kind, title)
	return chat.Step{}
}

// A step's detail: a command and what it printed with its exit code, a file's change, a note, an image read.
func TestDetail(t *testing.T) {
	p := db(t, "steps")
	id, _ := session(t, "steps")
	path := p + "#" + id
	r := &Reader{DB: p}

	d, err := r.Detail(path, stepOf(t, r, path, "run", "exit 3").ID)
	if err != nil || d.Run == nil {
		t.Fatalf("run: %+v %v", d, err)
	}
	if !d.Run.Failed || d.Run.Code != 3 || d.Run.Output != "checking\none test failed" || d.Run.Lines != 2 {
		t.Fatalf("run: %+v", d.Run)
	}

	d, err = r.Detail(path, stepOf(t, r, path, "edit", "notes.txt").ID)
	if err != nil || d.Change == nil {
		t.Fatalf("edit: %+v %v", d, err)
	}
	want := chat.Change{File: "notes.txt", Added: 2, Hunks: []chat.Hunk{{OldStart: 1, NewStart: 1, Lines: []string{" a", "+b", "+c"}}}}
	if !reflect.DeepEqual(*d.Change, want) {
		t.Fatalf("edit: %+v", *d.Change)
	}

	d, err = r.Detail(path, stepOf(t, r, path, "think", "Weighing").ID)
	if err != nil || !strings.HasPrefix(d.Output, "Weighing two plans") {
		t.Fatalf("think: %+v %v", d, err)
	}

	d, err = r.Detail(path, stepOf(t, r, path, "read", "shot.png").ID)
	if err != nil || d.Input != "shot.png" || len(d.Images) != 1 || d.Images[0].Source != "shot.png" {
		t.Fatalf("read: %+v %v", d, err)
	}
	media, b, err := r.Image(path, d.Images[0].ID)
	if err != nil || media != "image/png" || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Fatalf("image: %q %d %v", media, len(b), err)
	}
	if _, err := r.Detail(path, "call_nothing"); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("unknown step: %v", err)
	}
}

// The database is only read: the database and its WAL stay as they were. With OpenCode not running SQLite has
// removed its -wal and -shm, and a reader would make them: then it reads nothing.
func TestReadsOnly(t *testing.T) {
	p := db(t, "steps")
	id, _ := session(t, "steps")
	stamp := func(fs ...string) (s []string) {
		for _, f := range fs {
			if st, err := os.Stat(f); err == nil {
				s = append(s, fmt.Sprint(st.ModTime(), st.Size()))
			} else {
				s = append(s, "none")
			}
		}
		return
	}
	before := stamp(p, p+"-wal")
	r := &Reader{DB: p}
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{id}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Turns(p + "#" + id); err != nil {
		t.Fatal(err)
	}
	if after := stamp(p, p+"-wal"); !reflect.DeepEqual(before, after) {
		t.Fatalf("files changed: %v → %v", before, after)
	}

	stopped := filepath.Join(t.TempDir(), "opencode.db")
	d, _ := sql.Open("sqlite", stopped)
	schema, _ := os.ReadFile(filepath.Join("testdata", "opencode", "2.2.0", "schema.sql"))
	d.Exec("PRAGMA journal_mode=WAL;" + string(schema))
	add(t, d, "ses_a", "/work/acme", "A", "2.2.0", "")
	d.Close()
	before = stamp(stopped, stopped+"-wal", stopped+"-shm")
	r = &Reader{DB: stopped}
	if _, _, err := r.FindPane(chat.Pane{Cwd: "/work/acme", Sessions: []string{"ses_a"}}); reason(err) != chat.NoMatch {
		t.Fatalf("OpenCode not running: %v", err)
	}
	if _, err := r.Turns(stopped + "#ses_a"); err == nil {
		t.Fatal("OpenCode not running: Turns read")
	}
	if after := stamp(stopped, stopped+"-wal", stopped+"-shm"); !reflect.DeepEqual(before, after) {
		t.Fatalf("OpenCode not running, files changed: %v → %v", before, after)
	}
}

// OpenCode 1 reports no status to herdr-core (hc get: status null, foreground name "opencode", measured with
// 1.19.0): its terminal is known by that name.
func TestKnownByName(t *testing.T) {
	var r chat.Reader = &Reader{}
	if n, ok := r.(chat.Named); !ok || n.ProgramName() != "opencode" {
		t.Fatalf("not known by name: %v", ok)
	}
}
