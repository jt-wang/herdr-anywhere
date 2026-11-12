// Package chat turns an agent's own session record into turns for the phone's chat view. One Reader per agent
// answers two questions: which session file belongs to a process, and what that file says. The Service checks the
// record against what herdr-core says on screen, and answers "unknown" whenever it is not sure.
package chat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	Kind  string `json:"kind"` // read, edit, run, search, web, agent, note, think, other
	Title string `json:"title"`
	// Failed: the record says the step failed (a command's exit code, a refused or rejected call).
	Failed bool `json:"failed,omitempty"`
	// Added and Deleted: the lines an edit added and removed.
	Added   int `json:"added,omitempty"`
	Deleted int `json:"deleted,omitempty"`
}

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

// TailLines reads a JSON Lines file's last MaxTail bytes: its whole lines, and whether the window starts at the file's
// start. A line being written (no newline yet) is left for the next read.
func TailLines(path string) (ls [][]byte, from0 bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	start := st.Size() - MaxTail
	if start < 0 {
		start = 0
	}
	buf := make([]byte, st.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, false, err
	}
	if start > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return nil, false, Formatf("a line over %d bytes", MaxTail)
		}
		buf = buf[i+1:]
	}
	// The last line is complete only with its newline; one being written is left for the next read.
	if j := bytes.LastIndexByte(buf, '\n'); j >= 0 {
		buf = buf[:j]
	} else {
		buf = nil
	}
	for _, l := range bytes.Split(buf, []byte{'\n'}) {
		if len(bytes.TrimSpace(l)) > 0 {
			ls = append(ls, l)
		}
	}
	return ls, start == 0, nil
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
