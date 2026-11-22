package cursor

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

const fixtureID = "28fb8fac-b5d7-467d-8b2e-71f73224c3bc"

func TestSetupProcessStart(t *testing.T) {
	requireSupported(t)
	s := &Setup{Dir: t.TempDir(), Binary: filepath.Join(t.TempDir(), "companion"), HC: filepath.Join(t.TempDir(), "hc")}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	written := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(s.File(), written, written); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		start time.Time
		want  string
	}{
		{"already-running", written.Add(-time.Second), chat.SetupNotLoaded},
		{"same-time", written, chat.SetupNotLoaded},
		{"fresh-launch-or-resume", written.Add(time.Second), chat.AwaitingPrompt},
		{"unknown-start", time.Time{}, chat.SetupNotLoaded},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &Reader{Setup: s, started: func(pid int) time.Time {
				if pid != 42 {
					t.Fatalf("start time requested for %d, not the validated Cursor PID", pid)
				}
				return c.start
			}}
			for _, labels := range []map[string]string{nil, {chat.LabelPid: "42", chat.LabelSession: fixtureID}} {
				_, _, err := r.FindPane(chat.Pane{Pid: 42, Labels: labels})
				var miss *chat.Miss
				if !errors.As(err, &miss) || miss.Reason != c.want {
					t.Fatalf("got %v, want %s", err, c.want)
				}
			}
		})
	}
	r := &Reader{Setup: s, started: func(int) time.Time { return written.Add(time.Second) }}
	if err := os.Chtimes(s.File(), written.Add(2*time.Second), written.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.FindPane(chat.Pane{Pid: 42})
	var miss *chat.Miss
	if !errors.As(err, &miss) || miss.Reason != chat.SetupNotLoaded {
		t.Fatalf("rewritten hooks must require restart: %v", err)
	}
	if err := s.Remove(false); err != nil {
		t.Fatal(err)
	}
	_, _, err = r.FindPane(chat.Pane{Pid: 42})
	if !errors.As(err, &miss) || miss.Reason != chat.SetupNeeded {
		t.Fatalf("removed hooks must require setup: %v", err)
	}
}

func fixture(t *testing.T, name string) (*Reader, string, *sql.DB) {
	t.Helper()
	requireSupported(t)
	r := &Reader{Dir: t.TempDir()}
	cwd := filepath.Join(string(filepath.Separator), "home", "cedar", "paper-garden")
	path := filepath.Join(r.Dir, "chats", fmt.Sprintf("%x", md5.Sum([]byte(cwd))), fixtureID, "store.db")
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join("testdata", "2026.11.19-f005e9d", name))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	d, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	if _, e = d.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; UPDATE meta SET value=value"); e != nil {
		t.Fatal(e)
	}
	return r, path, d
}

func TestRealConversation(t *testing.T) {
	r, path, _ := fixture(t, "store.db")
	turns, e := r.Turns(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(turns) != 6 || turns[0].Said != "Reply exactly PAPER_HELLO. Do not use tools." || turns[0].Reply != "PAPER_HELLO" || turns[5].Reply != "PAPER_RESUMED" {
		t.Fatalf("turns: %+v", turns)
	}
	if turns[0].At != "2026-11-22T01:02:00Z" {
		t.Fatal("timestamp", turns[0].At)
	}
	counts := map[string]int{}
	runs, failed := 0, 0
	images := 0
	userImages := 0
	edits := 0
	for _, turn := range turns {
		for _, image := range turn.Images {
			userImages++
			media, data, err := r.Image(path, image.ID)
			if err != nil || media != "image/jpeg" || !bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
				t.Fatal("missing prompt image", media, data, err)
			}
		}
		if turn.End != "done" {
			t.Errorf("end %s", turn.End)
		}
		for _, step := range turn.Steps {
			counts[step.Kind]++
			detail, e := r.Detail(path, step.ID)
			if e != nil {
				t.Fatal(e)
			}
			if step.Kind == "run" {
				runs++
				if step.Failed {
					failed++
				}
				if detail.Run == nil || detail.Run.Failed != step.Failed {
					t.Fatalf("run %+v", detail)
				}
				if strings.Contains(step.Title, "SystemExit") {
					if detail.Run.Code != 7 || !step.Failed {
						t.Fatal("nonzero failure was lost")
					}
				}
				if strings.Contains(step.Title, "PAPER_DENIED") && (!step.Failed || !strings.Contains(detail.Output, "Rejected")) {
					t.Fatal("rejection was lost")
				}
				if strings.Contains(step.Title, "PAPER_APPROVED") && (step.Failed || !strings.Contains(detail.Run.Output, "PAPER_APPROVED")) {
					t.Fatal("stdout was lost")
				}
			}
			if detail.Change != nil {
				edits++
				if detail.Change.Added != 1 || detail.Change.Deleted != 1 || len(detail.Change.Hunks) != 1 {
					t.Fatalf("diff %+v", detail.Change)
				}
			}
			for _, image := range detail.Images {
				images++
				media, data, e := r.Image(path, image.ID)
				expected, _ := os.ReadFile("testdata/2026.11.19-f005e9d/garden.png")
				if e != nil || media != "image/png" || !bytes.Equal(data, expected) {
					t.Fatal("image mismatch", e)
				}
			}
		}
	}
	if runs != 3 || failed != 2 || images != 1 || userImages != 1 || edits != 1 || counts["read"] != 2 {
		t.Fatalf("coverage runs=%d failed=%d images=%d edits=%d kinds=%v", runs, failed, images, edits, counts)
	}
	if _, e = r.Detail(path, "absent"); !errors.Is(e, chat.ErrNoStep) {
		t.Fatal(e)
	}
	if _, _, e = r.Image(path, "absent"); !errors.Is(e, chat.ErrNoStep) {
		t.Fatal(e)
	}
	before, bpath, _ := fixture(t, "store-before-resume.db")
	old, e := before.Turns(bpath)
	if e != nil || len(old) != 5 || turns[0].ID != old[0].ID {
		t.Fatalf("resume %d %v", len(old), e)
	}
}

func TestHistoryFromRepeatedRealExport(t *testing.T) {
	r, path, db := fixture(t, "store.db")
	ms, err := r.messages(path)
	if err != nil {
		t.Fatal(err)
	}
	var root []byte
	for batch := 0; batch < 7; batch++ {
		for _, m := range ms {
			id, err := hex.DecodeString(m.id)
			if err != nil {
				t.Fatal(err)
			}
			root = append(root, 10, 32)
			root = append(root, id...)
		}
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	b, _ := hex.DecodeString(raw)
	var meta map[string]any
	_ = json.Unmarshal(b, &meta)
	if _, err := db.Exec(`UPDATE blobs SET data=? WHERE id=?`, root, meta["latestRootBlobId"]); err != nil {
		t.Fatal(err)
	}
	latest, err := r.Turns(path)
	if err != nil || len(latest) != chat.MaxTurns {
		t.Fatal("unbounded latest page", len(latest), err)
	}
	all := append([]chat.Turn(nil), latest...)
	before, err := r.Before(path, latest[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for before != "" {
		page, err := r.History(path, before)
		if err != nil || len(page.Turns) > chat.MaxTurns {
			t.Fatal("history page", err)
		}
		all = append(page.Turns, all...)
		before = page.Before
	}
	if len(all) != 42 {
		t.Fatal("history gaps or duplicates", len(all))
	}
	seen := map[string]bool{}
	for _, turn := range all {
		if seen[turn.ID] {
			t.Fatal("unstable turn identity", turn.ID)
		}
		seen[turn.ID] = true
	}
	if _, err := r.History(path, "missing-anchor"); !errors.Is(err, chat.ErrCursor) {
		t.Fatal("accepted invalid history anchor", err)
	}
	if p, err := r.History(path, all[0].ID); err != nil || len(p.Turns) != 0 || p.Before != "" {
		t.Fatal("oldest turn must have no earlier page", p, err)
	}
}

func TestSelectedPageByteBudget(t *testing.T) {
	r, path, db := fixture(t, "store.db")
	ms, err := r.messages(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE blobs SET data=json_set(data,'$.content[0].text',?) WHERE id=?`, strings.Repeat("x", chat.MaxBack+1), ms[0].id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.History(path, ""); !errors.Is(err, chat.ErrHistoryLimit) {
		t.Fatal("unbounded selected page", err)
	}
}

func TestOldAttachmentsDoNotBlockSelectedRecords(t *testing.T) {
	r, path, db := fixture(t, "store.db")
	ms, err := r.messages(path)
	if err != nil {
		t.Fatal(err)
	}
	var large []byte
	for _, m := range ms {
		var parts []map[string]any
		if m.Role != "user" || json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			if p["type"] != "image" {
				continue
			}
			var obj map[string]any
			if err := db.QueryRow(`SELECT data FROM blobs WHERE id=?`, m.id).Scan(&large); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal(large, &obj)
			content := obj["content"].([]any)
			for _, v := range content {
				part := v.(map[string]any)
				if part["type"] != "image" {
					continue
				}
				im := part["image"].(map[string]any)
				h := im["hex"].(string)
				im["hex"] = strings.Repeat(h, (1<<20)/len(h)+1)
			}
			large, _ = json.Marshal(obj)
		}
	}
	if len(large) < 1<<20 {
		t.Fatal("real export has no prompt image")
	}
	id := sha256.Sum256(large)
	if _, err := db.Exec(`INSERT INTO blobs(id,data) VALUES(?,?)`, hex.EncodeToString(id[:]), large); err != nil {
		t.Fatal(err)
	}
	var root []byte
	for i := 0; i < 35; i++ {
		root = append(root, 10, 32)
		root = append(root, id[:]...)
	}
	for _, m := range ms {
		b, _ := hex.DecodeString(m.id)
		root = append(root, 10, 32)
		root = append(root, b...)
	}
	for batch := 0; batch < 36; batch++ {
		for _, m := range ms[:2] {
			b, _ := hex.DecodeString(m.id)
			root = append(root, 10, 32)
			root = append(root, b...)
		}
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	b, _ := hex.DecodeString(raw)
	var meta map[string]any
	_ = json.Unmarshal(b, &meta)
	if _, err := db.Exec(`UPDATE blobs SET data=? WHERE id=?`, root, meta["latestRootBlobId"]); err != nil {
		t.Fatal(err)
	}
	if 35*len(large) <= chat.MaxBack {
		t.Fatal("fixture does not exceed the old session budget")
	}
	var latest []chat.Turn
	t.Run("latest", func(t *testing.T) {
		latest, err = r.Turns(path)
		if err != nil || len(latest) != chat.MaxTurns || latest[len(latest)-1].Reply != "PAPER_HELLO" {
			t.Fatal("old attachments blocked latest page", len(latest), err)
		}
	})
	oldPromptImage := ""
	t.Run("history", func(t *testing.T) {
		page, err := r.History(path, "")
		if err != nil || len(page.Turns) != chat.MaxTurns {
			t.Fatal("latest history page", err)
		}
		seen := map[string]bool{}
		for {
			for _, turn := range page.Turns {
				if seen[turn.ID] {
					t.Fatal("duplicate history turn", turn.ID)
				}
				seen[turn.ID] = true
				if len(turn.Images) > 0 {
					oldPromptImage = turn.Images[0].ID
				}
			}
			if page.Before == "" {
				break
			}
			page, err = r.History(path, page.Before)
			if err != nil {
				t.Fatal("old attachments blocked earlier page", err)
			}
		}
		if len(seen) != 77 {
			t.Fatal("history gaps", len(seen))
		}
	})
	for _, m := range ms {
		var parts []part
		if m.Role != "assistant" || json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			if p.Type != "tool-call" {
				continue
			}
			t.Run("detail-"+p.ToolCallID, func(t *testing.T) {
				d, err := r.Detail(path, p.ToolCallID)
				if err != nil || d.Input == "" {
					t.Fatal("old attachments blocked detail", err)
				}
				for _, image := range d.Images {
					mime, data, err := r.Image(path, image.ID)
					if err != nil || mime != "image/png" || len(data) == 0 {
						t.Fatal("old attachments blocked tool image", err)
					}
				}
			})
		}
	}
	t.Run("prompt-image", func(t *testing.T) {
		if oldPromptImage == "" {
			t.Fatal("missing older prompt image")
		}
		mime, data, err := r.Image(path, oldPromptImage)
		if err != nil || mime != "image/jpeg" || len(data) == 0 {
			t.Fatal("old attachments blocked prompt image", err)
		}
	})
	t.Run("tool-image", func(t *testing.T) {
		for _, m := range ms {
			var parts []part
			if m.Role != "tool" || json.Unmarshal(m.Content, &parts) != nil {
				continue
			}
			for _, p := range parts {
				for i, im := range p.Experimental {
					if im.Type != "image" {
						continue
					}
					mime, data, err := r.Image(path, fmt.Sprintf("%s/image/%d", p.ToolCallID, i))
					expected, _ := os.ReadFile("testdata/2026.11.19-f005e9d/garden.png")
					if err != nil || mime != "image/png" || !bytes.Equal(data, expected) {
						t.Fatal("old attachments blocked tool image", err)
					}
					return
				}
			}
		}
		t.Fatal("real export has no tool image")
	})
}

func TestPromptImageRejectsInvalidData(t *testing.T) {
	r, path, db := fixture(t, "store.db")
	normal, err := r.Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	imageID := ""
	for _, turn := range normal {
		if len(turn.Images) > 0 {
			imageID = turn.Images[0].ID
		}
	}
	if imageID == "" {
		t.Fatal("real export has no prompt image")
	}
	ms, err := r.messages(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		var parts []map[string]any
		if m.Role != "user" || json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for i, part := range parts {
			if part["type"] != "image" {
				continue
			}
			var original []byte
			if err := db.QueryRow(`SELECT data FROM blobs WHERE id=?`, m.id).Scan(&original); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"empty", "hex", "mime", "encoding"} {
				t.Run(bad, func(t *testing.T) {
					var obj map[string]any
					_ = json.Unmarshal(original, &obj)
					p := obj["content"].([]any)[i].(map[string]any)
					image := p["image"].(map[string]any)
					switch bad {
					case "empty":
						image["hex"] = ""
					case "hex":
						image["hex"] = "not-hex"
					case "mime":
						p["mimeType"] = "text/html"
					case "encoding":
						image["__type"] = "unknown"
					}
					b, _ := json.Marshal(obj)
					if _, err := db.Exec(`UPDATE blobs SET data=? WHERE id=?`, b, m.id); err != nil {
						t.Fatal(err)
					}
					turns, err := r.Turns(path)
					if err != nil || len(turns) != 6 {
						t.Fatal("malformed attachment discarded the prompt", err)
					}
					for _, turn := range turns {
						if len(turn.Images) != 0 {
							t.Fatal("accepted invalid prompt image", turn.Images)
						}
					}
					if _, _, err := r.Image(path, imageID); !errors.Is(err, chat.ErrNoStep) {
						t.Fatal("served invalid prompt image", err)
					}
				})
			}
			return
		}
	}
	t.Fatal("real export has no prompt image")
}

func TestRealResumeKeepsTurnIdentity(t *testing.T) {
	r, path, _ := fixture(t, "store-before-resume.db")
	before, err := r.Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	r, path, _ = fixture(t, "store.db")
	after, err := r.Turns(path)
	if err != nil || len(after) != len(before)+1 {
		t.Fatal("resume did not add one turn", err)
	}
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Fatal("resume changed an existing turn identity")
		}
	}
}

func TestOnlyLatestRootMessages(t *testing.T) {
	r, path, d := fixture(t, "store.db")
	if _, e := d.Exec(`INSERT INTO blobs VALUES (?,?)`, strings.Repeat("f", 64), []byte(`{"role":"user","content":[{"type":"text","text":"OBSOLETE_UNREACHABLE"}]}`)); e != nil {
		t.Fatal(e)
	}
	turns, e := r.Turns(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, turn := range turns {
		if strings.Contains(turn.Said, "OBSOLETE_UNREACHABLE") {
			t.Fatal("scanned an unreachable blob")
		}
	}
	var raw string
	if e = d.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	b, _ := hex.DecodeString(raw)
	var meta map[string]any
	_ = json.Unmarshal(b, &meta)
	if _, e = d.Exec(`UPDATE blobs SET data=? WHERE id=?`, []byte{}, meta["latestRootBlobId"]); e != nil {
		t.Fatal(e)
	}
	turns, e = r.Turns(path)
	if e != nil || len(turns) != 0 {
		t.Fatalf("empty active graph: %+v %v", turns, e)
	}
	if _, e = d.Exec(`UPDATE blobs SET data=? WHERE id=?`, []byte{10, 32, 1}, meta["latestRootBlobId"]); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Turns(path); e == nil {
		t.Fatal("accepted truncated protobuf")
	}
}

func TestCheckpointWrites(t *testing.T) {
	for _, name := range []string{"empty-root", "missing-metadata", "truncated-metadata", "missing-root", "truncated-root", "missing-message", "truncated-message", "wrong-identity", "invalid-root-id", "invalid-root", "invalid-message", "invalid-metadata", "invalid-missing-root-field", "invalid-null-root"} {
		t.Run(name, func(t *testing.T) {
			r, path, db := fixture(t, "store.db")
			good, err := r.Turns(path)
			if err != nil || len(good) == 0 {
				t.Fatal("fixture", good, err)
			}
			var raw string
			if err := db.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			decoded, _ := hex.DecodeString(raw)
			var meta map[string]any
			if err := json.Unmarshal(decoded, &meta); err != nil {
				t.Fatal(err)
			}
			rootID := meta["latestRootBlobId"].(string)
			var root, message []byte
			if err := db.QueryRow(`SELECT data FROM blobs WHERE id=?`, rootID).Scan(&root); err != nil {
				t.Fatal(err)
			}
			refs, err := promptRefs(root)
			if err != nil || len(refs) == 0 {
				t.Fatal("references", err)
			}
			if err := db.QueryRow(`SELECT data FROM blobs WHERE id=?`, refs[0]).Scan(&message); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "empty-root":
				meta["latestRootBlobId"] = ""
			case "missing-metadata":
				exec(`DELETE FROM meta WHERE key='0'`)
			case "truncated-metadata":
				exec(`UPDATE meta SET value=? WHERE key='0'`, hex.EncodeToString(decoded[:len(decoded)-1]))
			case "missing-root":
				exec(`DELETE FROM blobs WHERE id=?`, rootID)
			case "truncated-root":
				exec(`UPDATE blobs SET data=? WHERE id=?`, []byte{10, 32, 1}, rootID)
			case "missing-message":
				exec(`DELETE FROM blobs WHERE id=?`, refs[0])
			case "truncated-message":
				exec(`UPDATE blobs SET data=? WHERE id=?`, message[:len(message)-1], refs[0])
			case "wrong-identity":
				meta["agentId"] = "d74292da-735f-4f1c-99aa-3fd2a3719a70"
			case "invalid-root-id":
				meta["latestRootBlobId"] = "not-a-blob"
			case "invalid-missing-root-field":
				delete(meta, "latestRootBlobId")
			case "invalid-null-root":
				meta["latestRootBlobId"] = nil
			case "invalid-root":
				exec(`UPDATE blobs SET data=? WHERE id=?`, []byte{15}, rootID)
			case "invalid-message":
				exec(`UPDATE blobs SET data=? WHERE id=?`, []byte(`{"role":!}`), refs[0])
			case "invalid-metadata":
				exec(`UPDATE meta SET value=? WHERE key='0'`, hex.EncodeToString([]byte(`{"agentId":!}`)))
			}
			if name == "empty-root" || name == "wrong-identity" || name == "invalid-root-id" || name == "invalid-missing-root-field" || name == "invalid-null-root" {
				b, err := json.Marshal(meta)
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE meta SET value=? WHERE key='0'`, hex.EncodeToString(b))
			}
			written := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
			for _, file := range []string{path, path + "-wal"} {
				if err := os.Chtimes(file, written, written); err != nil {
					t.Fatal(err)
				}
			}
			for _, age := range []time.Duration{0, 4 * time.Second, 10 * time.Second} {
				r.now = func() time.Time { return written.Add(age) }
				_, err := r.Turns(path)
				var format *chat.FormatError
				var miss *chat.Miss
				wantFormat := strings.HasPrefix(name, "invalid-") || name == "wrong-identity" || age >= 10*time.Second && name != "empty-root"
				if wantFormat {
					if !errors.As(err, &format) {
						t.Fatalf("persistent or malformed %s at %v: got %v, want Format", name, age, err)
					}
				} else if !errors.As(err, &miss) || miss.Reason != chat.NoRecordYet {
					t.Fatalf("active write %s at %v: got %v, want retryable miss", name, age, err)
				}
			}
			exec(`INSERT OR REPLACE INTO meta VALUES ('0',?)`, raw)
			exec(`INSERT OR REPLACE INTO blobs VALUES (?,?)`, rootID, root)
			exec(`INSERT OR REPLACE INTO blobs VALUES (?,?)`, refs[0], message)
			got, err := r.Turns(path)
			if err != nil || !reflect.DeepEqual(good, got) {
				t.Fatal("completed checkpoint did not recover", err)
			}
		})
	}
}

func TestFindLayoutAndNoSideEffects(t *testing.T) {
	r, path, d := fixture(t, "store.db")
	cwd := filepath.Join(string(filepath.Separator), "home", "cedar", "paper-garden")
	p := chat.Pane{Pid: 42, Labels: map[string]string{chat.LabelPid: "42", chat.LabelSession: fixtureID, LabelCwd: cwd}}
	got, _, e := r.FindPane(p)
	if e != nil || got != path {
		t.Fatalf("find %q %v", got, e)
	}
	for _, id := range []string{"../escape", "", strings.Repeat("a", 36), strings.ToUpper(fixtureID)} {
		p.Labels[chat.LabelSession] = id
		if _, _, e = r.FindPane(p); e == nil {
			t.Fatalf("accepted id %q", id)
		}
	}
	p.Labels[chat.LabelSession] = fixtureID
	p.Labels[LabelCwd] = "relative"
	if _, _, e = r.FindPane(p); e == nil {
		t.Fatal("accepted relative cwd")
	}
	for _, path := range []string{filepath.Join(r.Dir, "store.db"), filepath.Join(r.Dir, "..", "store.db"), filepath.Join(r.Dir, "chats", "bad", fixtureID, "store.db")} {
		if _, e = r.Turns(path); e == nil {
			t.Fatal("accepted bad path", path)
		}
	}
	outside := t.TempDir()
	link := filepath.Join(r.Dir, "chats", strings.Repeat("a", 32))
	if e := os.Symlink(outside, link); e == nil {
		escaped := filepath.Join(link, fixtureID, "store.db")
		if e := os.MkdirAll(filepath.Dir(escaped), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(escaped, []byte("not a database"), 0600); e != nil {
			t.Fatal(e)
		}
		if r.valid(escaped) {
			t.Fatal("accepted symlink escape")
		}
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Turns(path); e == nil {
		t.Fatal("read closed DB without WAL")
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if _, e = os.Stat(path + ext); !os.IsNotExist(e) {
			t.Fatalf("created %s: %v", ext, e)
		}
	}
}

func TestConfigRoot(t *testing.T) {
	t.Setenv("CURSOR_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	h, _ := os.UserHomeDir()
	if ConfigRoot() != filepath.Join(h, ".cursor") {
		t.Fatal(ConfigRoot())
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if ConfigRoot() != filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "cursor") {
		t.Fatal(ConfigRoot())
	}
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(t.TempDir(), "cursor"))
	if ConfigRoot() != os.Getenv("CURSOR_CONFIG_DIR") {
		t.Fatal(ConfigRoot())
	}
}
