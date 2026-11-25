package codex

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const titleIndexLimit = 16 << 20
const titleLineLimit = 256 << 10

// With a live Codex 0.167.1 rollout (title_test.go has its shape): session_meta.payload.id
// matched session_index.jsonl id and returned its existing thread_name.
// Title reads only the selected rollout's metadata and the bounded local name index.
// A different session in the same folder is never a fallback.
func (r *Reader) Title(path string) (string, error) {
	dir, e := filepath.EvalSymlinks(r.Dir)
	if e != nil {
		return "", e
	}
	actual, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	sessions := filepath.Join(dir, "sessions")
	rel, e := filepath.Rel(sessions, actual)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !rollout.MatchString(filepath.ToSlash(actual)) {
		return "", nil
	}
	info, e := os.Stat(actual)
	if e != nil || !info.Mode().IsRegular() {
		return "", e
	}
	f, e := os.Open(actual)
	if e != nil {
		return "", e
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		f.Close()
		return "", e
	}
	scan := bufio.NewScanner(io.LimitReader(f, titleLineLimit+1))
	scan.Buffer(make([]byte, 4096), titleLineLimit)
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ID     string          `json:"id"`
			Source json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	valid := scan.Scan() && json.Unmarshal(scan.Bytes(), &meta) == nil && meta.Type == "session_meta" && meta.Payload.ID != ""
	f.Close()
	if !valid {
		return "", nil
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(meta.Payload.Source, &source) == nil && source["subagent"] != nil {
		return "", nil
	}
	index, e := filepath.EvalSymlinks(filepath.Join(dir, "session_index.jsonl"))
	if e != nil {
		return "", e
	}
	rel, e = filepath.Rel(dir, index)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil
	}
	info, e = os.Stat(index)
	if e != nil || !info.Mode().IsRegular() || info.Size() > titleIndexLimit {
		return "", e
	}
	f, e = os.Open(index)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e = f.Stat()
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() || st.Size() > titleIndexLimit {
		return "", nil
	}
	limited := &io.LimitedReader{R: f, N: titleIndexLimit + 1}
	scan = bufio.NewScanner(limited)
	scan.Buffer(make([]byte, 4096), titleLineLimit)
	var title string
	for scan.Scan() {
		var row struct {
			ID   string  `json:"id"`
			Name *string `json:"thread_name"`
		}
		if json.Unmarshal(scan.Bytes(), &row) == nil && row.ID == meta.Payload.ID && row.Name != nil {
			title = *row.Name
		}
	}
	if e = scan.Err(); e != nil {
		return "", e
	}
	if limited.N == 0 {
		return "", nil
	}
	return title, nil
}
