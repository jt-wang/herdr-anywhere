// Package chat turns an agent's own session record into turns for the phone's chat view. One Reader per agent
// answers two questions: which session file belongs to a process, and what that file says. The Service checks the
// record against what herdr-core says on screen, and answers "unknown" whenever it is not sure.
package chat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Turn is one exchange: what the person (or another program) said, the steps the agent took, its reply.
type Turn struct {
	ID   string `json:"id"`
	From string `json:"from"` // "you", or "other" for a task notification or another agent's message
	Said string `json:"said"`
	// At is when it was said (RFC 3339), as the record has it.
	At string `json:"at,omitempty"`
	// Images are the images sent with it.
	Images []Image `json:"images,omitempty"`
	Steps  []Step  `json:"steps"`
	Reply  string  `json:"reply"`
	Ask    *Ask    `json:"ask,omitempty"`
	End    string  `json:"end"` // "open", "done", or "stopped" (interrupted, or an error)
	Secs   int     `json:"secs,omitempty"`
}

// Step is one line: its kind and a short title. Its details are fetched only when the phone asks (Detail).
type Step struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // read, edit, run, search, web, agent, note, think, other, you (StepYou)
	Title string `json:"title"`
	// Failed: the record says the step failed (a command's exit code, a refused or rejected call).
	Failed bool `json:"failed,omitempty"`
	// Added and Deleted: the lines an edit added and removed.
	Added   int `json:"added,omitempty"`
	Deleted int `json:"deleted,omitempty"`
}

// StepYou is a step that is a message the person sent while the agent worked, which the agent took between two
// steps of the same turn (Claude Code's queued messages, Codex's and Pi's steering, OpenCode's queued prompts). Its
// Title is the whole message.
const StepYou = "you"

// Ask is a question the agent asked and nobody has answered yet.
type Ask struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// Detail is one step's input and output, cut to MaxDetail bytes each.
type Detail struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	// Images are the images in its result.
	Images []Image `json:"images,omitempty"`
	// What the step was, when the record says it plainly enough for the phone to show it as such: a command and what
	// it printed, a change to a file, the lines of a file it read. Input and Output stay, for any other step.
	Run    *Run    `json:"run,omitempty"`
	Change *Change `json:"change,omitempty"`
	Read   *Read   `json:"read,omitempty"`
}

// Run is a command the agent ran: whether it failed, its exit code when the record says, and what it printed (the end
// of it, MaxDetail bytes at most; Lines counts all of it).
type Run struct {
	Command string `json:"command"`
	Failed  bool   `json:"failed"`
	Code    int    `json:"code,omitempty"`
	Output  string `json:"output"`
	Lines   int    `json:"lines"`
}

// Change is what a step changed in a file: its hunks, each line marked " ", "+" or "-" as in a unified diff.
type Change struct {
	File string `json:"file"`
	// Cut: there were more than MaxChangeLines lines; the rest are left out.
	Cut     bool   `json:"cut,omitempty"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Hunks   []Hunk `json:"hunks"`
}

// Hunk is one place a file changed, by its first line before and after.
type Hunk struct {
	OldStart int      `json:"oldStart"`
	NewStart int      `json:"newStart"`
	Lines    []string `json:"lines"`
}

// Read is which lines of a file a step read.
type Read struct {
	File  string `json:"file"`
	From  int    `json:"from"`
	Count int    `json:"count"`
	Total int    `json:"total"`
}

// Image is an image of a turn or a step: its id for Reader.Image, and where it came from (a file's path) when the
// record says, shown above it so it can be copied.
type Image struct {
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
}

// ImageTypes are the image types the phone is sent; another type stays "[image]".
var ImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

const (
	MaxDetail = 4 << 10
	// MaxChangeLines is how many lines of a change go to the phone.
	MaxChangeLines = 400
	MaxTurns       = 30
	// MaxTail is how much of a session file is read, from its end.
	MaxTail = 4 << 20
	// MaxBack is how far back from a record's end Start looks for its last turn's first line, when the last MaxTail
	// bytes don't hold it: a Claude Code record whose turn ran large tool outputs had its last prompt 6.1 MB from the
	// end, its longest line 2.3 MB.
	MaxBack = 32 << 20
)

// Reader is one agent's: where its session record is and what it says.
type Reader interface {
	// Find returns the session file of the process pid, and the agent's version as the record says it.
	// It returns a *Miss when there is no sure match.
	Find(pid int) (path, version string, err error)
	// Turns reads the file's last turns. A format it doesn't know is a *FormatError.
	Turns(path string) ([]Turn, error)
	// Detail finds one step again by its id.
	Detail(path, step string) (Detail, error)
	// Image finds an image of a turn or of a step's result again by its id: its type and bytes.
	Image(path, id string) (media string, data []byte, err error)
}

// Named is a Reader of a program that reports no status to herdr-core (OpenCode 1): its terminal is known by the
// name of the program in its foreground, and its record alone says whether a turn is open.
type Named interface {
	ProgramName() string
}

// Miss is a reason there is no conversation to show.
type Miss struct{ Reason string }

func (m *Miss) Error() string { return m.Reason }

// Reasons a Miss or an answer gives, as the phone gets them.
const (
	NoTerminal  = "no-terminal"   // hc can't say what runs there
	NoMatch     = "no-match"      // no session file belongs to this process for sure
	NoRecordYet = "no-record-yet" // the agent writes its record at the first prompt
	NoRecord    = "no-record"     // the agent runs with its record turned off: none will come
	Ambiguous   = "ambiguous"
	Behind      = "behind" // the record says something else than the screen
	Format      = "format" // a format the reader doesn't know
	NoStatus    = "no-status"
	// NotReported: an agent that says which conversation it shows only while it works hasn't said it yet to this
	// Companion; its next turn will.
	NotReported = "not-reported"
	// An agent that says which conversation it shows only through a Setup's file (Answer.Setup names the agent):
	SetupNeeded    = "setup-needed"     // the file isn't there: the phone offers to put it there
	SetupNotLoaded = "setup-not-loaded" // it is, but this agent started before it: its Reload, or a restart
)

// FormatError names the field the reader didn't know, for the log.
type FormatError struct{ Field string }

func (e *FormatError) Error() string { return "unknown format: " + e.Field }

// Formatf makes a FormatError.
func Formatf(f string, a ...any) error { return &FormatError{Field: fmt.Sprintf(f, a...)} }

// ErrNoStep: the step (or image) id is not in the file (it changed, or the id is wrong).
var ErrNoStep = errors.New("no such step")

// Cut shortens s to at most n bytes on a rune boundary, marking the cut.
// CutEnd keeps the last n bytes of s (from a whole character), "…" in front when it cut: what a command printed last.
func CutEnd(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && s[i]&0xC0 == 0x80 {
		i++
	}
	return "…" + s[i:]
}

func Cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := n
	for i > 0 && s[i]&0xC0 == 0x80 {
		i--
	}
	return s[:i] + "…"
}

var hunkHead = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// DiffHunks: a unified diff's hunks, MaxChangeLines lines at most (cut: there were more).
func DiffHunks(diff string) (hunks []Hunk, cut bool) {
	hunks = []Hunk{}
	n := 0
	var h *Hunk
	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if m := hunkHead.FindStringSubmatch(l); m != nil {
			o, _ := strconv.Atoi(m[1])
			nw, _ := strconv.Atoi(m[2])
			hunks = append(hunks, Hunk{OldStart: o, NewStart: nw})
			h = &hunks[len(hunks)-1]
			continue
		}
		if h == nil || strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") || l == "" || !strings.ContainsAny(l[:1], " +-") {
			continue
		}
		if n >= MaxChangeLines {
			return hunks, true
		}
		n++
		h.Lines = append(h.Lines, l)
	}
	return hunks, false
}

// Start is where a reader reads a record from: its last MaxTail bytes, or further back to the start of the last line
// that begins a turn, when that line is older; never more than MaxBack from the end. A turn's first line is a line
// holding mark (as the record writes it, so the same words quoted inside a tool's output, escaped, don't match) that
// isStart accepts. from0: reading starts at the file's start. The file is searched backwards a chunk at a time; only
// a line holding mark is read whole.
func Start(path string, mark []byte, isStart func([]byte) bool) (off int64, from0 bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false, err
	}
	size := st.Size()
	tail := max(size-MaxTail, 0)
	floor := max(size-MaxBack, 0)
	const chunk = 1 << 20
	buf := make([]byte, chunk+len(mark))
	for end := size; end > floor; {
		from := max(end-chunk, floor)
		n := int(min(end+int64(len(mark)), size) - from) // overlapping the next chunk, for a mark across the two
		if _, err := f.ReadAt(buf[:n], from); err != nil && err != io.EOF {
			return 0, false, err
		}
		for b := buf[:n]; ; {
			i := bytes.LastIndex(b, mark)
			if i < 0 {
				break
			}
			b = b[:i]
			if line, at, err := lineAt(f, size, from+int64(i)); err == nil && isStart(line) {
				return min(at, tail), min(at, tail) == 0, nil
			}
		}
		end = from
	}
	return tail, tail == 0, nil
}

// lineAt reads the whole line holding the byte at pos, and where it starts.
func lineAt(f *os.File, size, pos int64) ([]byte, int64, error) {
	var b [4096]byte
	start := pos
	for start > 0 {
		from := max(start-int64(len(b)), 0)
		n, err := f.ReadAt(b[:start-from], from)
		if err != nil && err != io.EOF {
			return nil, 0, err
		}
		if i := bytes.LastIndexByte(b[:n], '\n'); i >= 0 {
			start = from + int64(i) + 1
			break
		}
		start = from
	}
	r := bufio.NewReader(io.NewSectionReader(f, start, size-start))
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, 0, err // no newline: the line is still being written
	}
	return line[:len(line)-1], start, nil
}

// Lines is EachLine as a loop: for l := range Lines(path, off, &err) { … }. err is set when reading fails.
func Lines(path string, off int64, err *error) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		e := EachLine(path, off, func(l []byte) error {
			if !yield(l) {
				return errStop
			}
			return nil
		})
		if e != nil && e != errStop {
			*err = e
		}
	}
}

var errStop = errors.New("stop")

// EachLine calls fn with each whole line from off: a line off cuts into is skipped (unless off is 0 or follows a
// newline), and one still being written (no newline yet) is left for the next read. One line at a time is held, so
// a reader's memory is its longest line, not what it reads.
func EachLine(path string, off int64, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	cut := false
	if off > 0 {
		var b [1]byte
		if _, err := f.ReadAt(b[:], off-1); err != nil {
			return err
		}
		cut = b[0] != '\n'
	}
	r := bufio.NewReaderSize(io.NewSectionReader(f, off, st.Size()-off), 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			return nil // the end, or a line being written
		}
		if err != nil {
			return err
		}
		if cut {
			cut = false
			continue
		}
		if l := bytes.TrimSpace(line); len(l) > 0 {
			if err := fn(l); err != nil {
				return err
			}
		}
	}
}
