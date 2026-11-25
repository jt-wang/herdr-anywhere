package pi

import (
	"bytes"
	"encoding/json"
	"herdr-anywhere/companion/internal/chat"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const titleWindow = 4 << 20
const titleLine = 64 << 10

// Title reads session naming metadata only. Pi session_info.name is session-wide,
// independent of the active branch. OMP's physical title slot precedes its logical header.
func (r *Reader) Title(path string) (string, error) {
	file := recordPath(path)
	if r.Setup == nil || filepath.Ext(file) != ".jsonl" || !r.containedSession(file) {
		return "", nil
	}
	st, e := os.Stat(file)
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}
	f, e := os.Open(file)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e = f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return "", e
	}

	if parts := strings.Split(path, "\x00"); len(parts) > 1 {
		if len(parts) != 3 || !leafSnapshot.MatchString(parts[1]) {
			return "", nil
		}
		inode, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil || inode != chat.Inode(st) {
			return "", nil
		}
	}
	prefix := make([]byte, min(st.Size(), int64(titleLine)))
	n, e := f.ReadAt(prefix, 0)
	if e != nil && e != io.EOF {
		return "", e
	}
	prefix = prefix[:n]
	type row struct {
		Type  string  `json:"type"`
		ID    string  `json:"id"`
		Title *string `json:"title"`
		Name  *string `json:"name"`
	}
	var slot *string
	var header row
	for i := 0; i < 2 && len(prefix) > 0; i++ {
		var line []byte
		line, prefix, _ = bytes.Cut(prefix, []byte("\n"))
		var x row
		if json.Unmarshal(line, &x) != nil {
			return "", nil
		}
		if i == 0 && x.Type == "title" && r.Setup.Agent() == "omp" {
			slot = x.Title
			continue
		}
		if x.Type != "session" || x.ID == "" {
			return "", nil
		}
		header = x
		break
	}
	if header.Type != "session" {
		return "", nil
	}
	if r.Setup.Agent() == "omp" {
		if slot != nil {
			return *slot, nil
		}
		if header.Title != nil {
			return *header.Title, nil
		}
		return "", nil
	}
	start := max(int64(0), st.Size()-titleWindow)
	data := make([]byte, st.Size()-start)
	n, e = f.ReadAt(data, start)
	if e != nil && e != io.EOF {
		return "", e
	}
	data = data[:n]
	if start > 0 {
		_, data, _ = bytes.Cut(data, []byte("\n"))
	}
	var name string
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		if len(line) > titleLine {
			continue
		}
		var x row
		if json.Unmarshal(line, &x) == nil && x.Type == "session_info" && x.Name != nil {
			name = *x.Name
		}
	}
	return name, nil
}
