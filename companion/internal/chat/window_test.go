package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A made-up record: JSON lines, some of them turn starts ({"start":true}), the others filler of the given sizes.
func record(t *testing.T, parts ...any) string {
	t.Helper()
	var b bytes.Buffer
	for _, p := range parts {
		switch x := p.(type) {
		case string: // a turn start, named
			fmt.Fprintf(&b, `{"start":true,"name":%q}`+"\n", x)
		case bool: // a line holding the mark that is not a turn start
			b.WriteString(`{"start":false}` + "\n")
		case int: // a line of filler about this many bytes, its text quoting a start as a tool's output would
			pad := strings.Repeat("x", x)
			l, _ := json.Marshal(map[string]string{"out": `{"start":true} ` + pad})
			b.Write(l)
			b.WriteByte('\n')
		}
	}
	p := filepath.Join(t.TempDir(), "r.jsonl")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func isStart(l []byte) bool {
	var d struct {
		Start bool `json:"start"`
	}
	return json.Unmarshal(l, &d) == nil && d.Start
}

func lines(t *testing.T, p string) (names []string, n int, longest int) {
	t.Helper()
	off, _, err := Start(p, []byte(`"start"`), isStart)
	if err != nil {
		t.Fatal(err)
	}
	err = EachLine(p, off, func(l []byte) error {
		n++
		if len(l) > longest {
			longest = len(l)
		}
		var d struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(l, &d) == nil && d.Name != "" {
			names = append(names, d.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// The last turn began further back than MaxTail (its tool outputs are large): reading starts at its first line.
func TestStartGoesBackToTheLastTurn(t *testing.T) {
	p := record(t, "one", 1000, "two", 1<<20, 1<<20, false, 1<<20, 1<<20, 1<<20, 1<<20, 500)
	names, _, _ := lines(t, p)
	if strings.Join(names, " ") != "two" {
		t.Fatalf("read the turns %v, want the last one from its start", names)
	}
}

// Within MaxTail, the window is MaxTail as before: earlier turns too.
func TestStartKeepsTheTail(t *testing.T) {
	p := record(t, "one", 1000, "two", 1000, "three", 1000)
	names, _, _ := lines(t, p)
	if strings.Join(names, " ") != "one two three" {
		t.Fatalf("read %v", names)
	}
}

// No turn start within MaxBack: reading starts MaxTail from the end, as before (the reader shows what it has).
func TestStartStopsAtMaxBack(t *testing.T) {
	parts := []any{"one"}
	for i := 0; i < (MaxBack>>20)+2; i++ {
		parts = append(parts, 1<<20)
	}
	parts = append(parts, 300)
	p := record(t, parts...)
	off, _, err := Start(p, []byte(`"start"`), isStart)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Size()-off > MaxTail {
		t.Fatalf("read from %d bytes before the end, more than MaxTail", st.Size()-off)
	}
	names, n, _ := lines(t, p)
	if len(names) != 0 || n == 0 {
		t.Fatalf("names %v, %d lines", names, n)
	}
}

// One line over MaxTail (a tool's output of 5 MB): no error, the line is read whole when the turn starts before it.
func TestALineOverMaxTail(t *testing.T) {
	p := record(t, "one", "two", 5<<20, 200)
	names, n, longest := lines(t, p)
	if strings.Join(names, " ") != "two" || n != 3 || longest < 5<<20 {
		t.Fatalf("names %v, %d lines, the longest %d bytes", names, n, longest)
	}
	// And when only that line's end is in the window, with no start found: no error, nothing cut is read.
	q := record(t, 5<<20, 200)
	if _, n, _ := lines(t, q); n != 1 {
		t.Fatalf("%d lines from a window inside a long line, want the one after it", n)
	}
}

// A line still being written (no newline yet) is left for the next read.
func TestEachLineLeavesTheLineBeingWritten(t *testing.T) {
	p := record(t, "one", 100)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"start":true,"name":"half`)
	f.Close()
	names, n, _ := lines(t, p)
	if strings.Join(names, " ") != "one" || n != 2 {
		t.Fatalf("names %v, %d lines", names, n)
	}
}

// The turn's start line split by the edge of the chunks Start searches in (1 MB each, from the end): still found.
func TestStartAcrossChunks(t *testing.T) {
	for _, k := range []int{1, 3, 5} { // the edge falls inside "start", k bytes after its opening quote
		var b bytes.Buffer
		b.WriteString(`{"start":true,"name":"one"}` + "\n")
		b.WriteString(`{"start":true,"name":"two"}` + "\n")
		at := bytes.LastIndex(b.Bytes(), []byte(`"start"`)) + k
		// After it: lines of filler so that the file ends 5 MB after that point.
		rest := 5<<20 - (b.Len() - at)
		for rest > 0 {
			n := min(rest, 1<<20) - 1
			l := bytes.Repeat([]byte("x"), n)
			l[0] = '"'
			l[n-1] = '"'
			b.Write(l)
			b.WriteByte('\n')
			rest -= n + 1
		}
		p := filepath.Join(t.TempDir(), "r.jsonl")
		if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(p); st.Size()-int64(at) != 5<<20 {
			t.Fatalf("built %d bytes after the edge", st.Size()-int64(at))
		}
		if names, _, _ := lines(t, p); strings.Join(names, " ") != "two" {
			t.Fatalf("split %d bytes in: read %v", k, names)
		}
	}
}
