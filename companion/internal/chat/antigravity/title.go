package antigravity

import (
	"context"
	"database/sql"
	"herdr-anywhere/companion/internal/chat"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Title reads the name indexed by agy for the conversation identified by its presence lock.
// The conversation_summaries schema as Antigravity CLI writes it (title_test.go); no preview, summary blob or message body is queried.
func (r *Reader) Title(path string) (string, error) {
	dir := r.Dir
	if dir == "" {
		dir = Dir()
	}
	root, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return "", e
	}
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, real)
	if e != nil {
		return "", e
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 5 || parts[0] != "brain" || !conversationID.MatchString(parts[1]) || parts[2] != ".system_generated" || parts[3] != "logs" || parts[4] != "transcript.jsonl" {
		return "", nil
	}
	db := filepath.Join(root, "conversation_summaries.db")
	actual, e := filepath.EvalSymlinks(db)
	if e != nil || actual != db {
		return "", e
	}
	st, e := os.Stat(db)
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	_, wal := os.Stat(db + "-wal")
	_, shm := os.Stat(db + "-shm")
	options := "?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)"
	if os.IsNotExist(wal) && os.IsNotExist(shm) {
		options += "&immutable=1"
	} else if wal != nil || shm != nil {
		return "", nil
	}
	uri := chat.SQLiteFileURI(filepath.ToSlash(db))
	d, e := sql.Open("sqlite", uri+options)
	if e != nil {
		return "", e
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var title sql.NullString
	e = d.QueryRowContext(ctx, "SELECT substr(title,1,1024) FROM conversation_summaries WHERE conversation_id=? AND (parent_conversation_id IS NULL OR parent_conversation_id='') LIMIT 1", parts[1]).Scan(&title)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return title.String, e
}
