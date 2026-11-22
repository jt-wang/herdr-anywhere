package pi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"herdr-anywhere/companion/internal/chat"
)

// Search backward in fixed windows, retaining one line, and never follow appends beyond end.
func entryEnd(path, id string, end int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if end < 0 {
		end = st.Size()
	}
	if end > st.Size() {
		return 0, chat.ErrCursor
	}
	for upper := end; upper > 0; {
		lower := max(upper-chat.MaxBack, 0)
		r := bufio.NewReaderSize(io.NewSectionReader(f, lower, end-lower), 64<<10)
		pos := lower
		cut := false
		if lower > 0 {
			var previous [1]byte
			if _, err := f.ReadAt(previous[:], lower-1); err != nil {
				return 0, err
			}
			cut = previous[0] != '\n'
		}
		for pos < upper {
			if cut {
				fragment, err := r.ReadSlice('\n')
				pos += int64(len(fragment))
				if err == bufio.ErrBufferFull {
					continue
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					return 0, err
				}
				cut = false
				continue
			}
			line, err := r.ReadBytes('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, err
			}
			pos += int64(len(line))
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var e struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(line, &e) != nil {
				return 0, chat.Formatf("a line that isn't JSON")
			}
			if e.ID == id {
				return pos, nil
			}
		}
		upper = lower
	}
	return 0, chat.ErrCursor
}
