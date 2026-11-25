package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const titleTailLimit = 4 << 20
const titleLineLimit = 64 << 10

// Title reads existing display metadata from the matched root transcript only.
// Claude Code 2.1.330: ai-title records contain aiTitle and sessionId (title_test.go).
// The latest explicit custom title takes precedence over an automatically generated name.
// Only the last 4 MiB are searched: older metadata yields no fallback, never another session's title.
func (r *Reader) Title(path string) (string, error) {
	dir, e := filepath.EvalSymlinks(r.Dir)
	if e != nil {
		return "", e
	}
	actual, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(filepath.Join(dir, "projects"), actual)
	if e != nil {
		return "", e
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == ".." || filepath.Ext(parts[1]) != ".jsonl" {
		return "", nil
	}
	sid := strings.TrimSuffix(parts[1], ".jsonl")
	if !sessionID.MatchString(sid) {
		return "", nil
	}
	st, e := os.Stat(actual)
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	f, e := os.Open(actual)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e = f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	size := st.Size()
	start := max(int64(0), size-titleTailLimit)
	data := make([]byte, size-start)
	n, e := f.ReadAt(data, start)
	if e != nil && e != io.EOF {
		return "", e
	}
	data = data[:n]
	if start > 0 {
		_, data, _ = bytes.Cut(data, []byte("\n"))
	}
	var ai, custom string
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		if len(line) > titleLineLimit {
			continue
		}
		// Unmarshal only metadata fields; message/tool content is never interpreted as a title.
		var row struct {
			Type      string  `json:"type"`
			ID        string  `json:"sessionId"`
			AI        *string `json:"aiTitle"`
			Custom    *string `json:"customTitle"`
			Sidechain bool    `json:"isSidechain"`
		}
		if json.Unmarshal(line, &row) != nil || row.ID != sid || row.Sidechain {
			continue
		}
		switch row.Type {
		case "ai-title":
			if row.AI != nil {
				ai = *row.AI
			}
		case "custom-title":
			if row.Custom != nil {
				custom = *row.Custom
			}
		}
	}
	if strings.TrimSpace(custom) != "" {
		return custom, nil
	}
	return ai, nil
}
