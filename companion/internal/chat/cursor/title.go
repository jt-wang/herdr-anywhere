package cursor

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"herdr-anywhere/companion/internal/chat"
	"os"
	"path/filepath"
	"time"
)

// Title reads the existing meta.name in the same validated database as the chat reader.
func (r *Reader) Title(path string) (string, error) {
	if !r.valid(path) {
		return "", nil
	}
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	options := "?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)"
	_, wal := os.Stat(path + "-wal")
	_, shm := os.Stat(path + "-shm")
	if os.IsNotExist(wal) && os.IsNotExist(shm) {
		options += "&immutable=1"
	} else if wal != nil || shm != nil {
		return "", nil
	}
	uri := chat.SQLiteFileURI(filepath.ToSlash(path))
	d, e := sql.Open("sqlite", uri+options)
	if e != nil {
		return "", e
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var raw string
	e = d.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='0' AND length(value)<=524288").Scan(&raw)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	data, e := hex.DecodeString(raw)
	if e != nil {
		return "", e
	}
	var m struct {
		ID   string `json:"agentId"`
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &m) != nil || m.ID != filepath.Base(filepath.Dir(path)) {
		return "", nil
	}
	return m.Name, nil
}
