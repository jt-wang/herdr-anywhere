package opencode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

// Title queries only the already matched session's display name, never messages.
func (r *Reader) Title(path string) (string, error) {
	p, id, e := split(path)
	if e != nil || id == "" {
		return "", e
	}
	real, e := filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	expected, e := filepath.EvalSymlinks(r.db())
	if e != nil || real != expected {
		return "", e
	}
	st, e := os.Stat(real)
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	d, e := open(p)
	if e != nil {
		return "", e
	}
	defer d.Close()
	tableName := "session_v2"
	if !table(d, tableName) {
		tableName = "session"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var title sql.NullString
	e = d.QueryRowContext(ctx, "SELECT substr(title,1,1024) FROM "+tableName+" WHERE id=? AND parent_id IS NULL LIMIT 1", id).Scan(&title)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return title.String, e
}
