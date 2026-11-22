package pi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

type runtimeLeaf struct {
	snapshot string
	inode    uint64
	size     int64
	mod      time.Time
	checked  int64
	expired  bool
}

func (r *Reader) expiredLeaf(file, snapshot string, st os.FileInfo) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, ok := r.leaves[file]
	same := ok && previous.snapshot == snapshot && previous.inode == chat.Inode(st)
	if same && previous.size == st.Size() && previous.mod.Equal(st.ModTime()) {
		return previous.expired, nil
	}
	current := runtimeLeaf{snapshot: snapshot, inode: chat.Inode(st), size: st.Size(), mod: st.ModTime()}
	if same && st.Size() > previous.size {
		current.checked, current.expired = previous.checked, previous.expired
	} else {
		_, tail, _ := strings.Cut(snapshot, ":")
		if tail != "~" {
			off, err := entryEnd(file, tail, st.Size())
			if errors.Is(err, chat.ErrCursor) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			current.checked = off
		}
	}
	if !current.expired {
		f, err := os.Open(file)
		if err != nil {
			return false, err
		}
		defer f.Close()
		rd := bufio.NewReader(io.NewSectionReader(f, current.checked, st.Size()-current.checked))
		for {
			line, err := rd.ReadBytes('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				return false, err
			}
			current.checked += int64(len(line))
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var e struct {
				Type       string `json:"type"`
				CustomType string `json:"customType"`
			}
			if json.Unmarshal(line, &e) != nil {
				return false, chat.Formatf("a line that isn't JSON")
			}
			if e.Type == "custom" && e.CustomType == "session_exit" {
				current.expired = true
				break
			}
		}
	}
	if r.leaves == nil {
		r.leaves = map[string]runtimeLeaf{}
	}
	r.leaves[file] = current
	return current.expired, nil
}
