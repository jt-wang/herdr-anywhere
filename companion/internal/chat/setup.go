package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A Setup is a small file Companion puts in an agent's own folder so the chat can tell which conversation a pane's
// agent writes: for an agent that keeps no file open and reports no conversation to herdr-core (Pi). The file sets
// the terminal's labels LabelPid and LabelSession; FindPane readers take them only from the process running there.
//
// Setups are listed by agent in one map (serve's chatSetups): adding an agent's is one entry, retiring one is
// taking its entry out. The phone and the API know no agent by name; they show what Info says.
type Setup interface {
	Agent() string // the app name the agent reports in OSC 7501, as in Service.Readers
	Name() string  // as people call it: "Pi"
	File() string  // where the file goes on this computer
	Reload() string
	Version() int // of the file this Companion writes
	State() SetupState
	Install(force bool) error
	Remove(force bool) error
}

// SetupState is the file's state on this computer.
type SetupState string

const (
	NoAgent   SetupState = "no-agent"  // the agent's folder isn't there: it isn't on this computer; nothing is offered
	Absent    SetupState = "absent"    // the agent is there, the file isn't
	Installed SetupState = "installed" // the file is there, as this Companion writes it
	Old       SetupState = "old"       // ours, from another Companion version or hc path: installing again updates it
	Changed   SetupState = "changed"   // a file of that name that isn't ours as written: someone changed it
)

var (
	ErrNoAgent = errors.New("the agent isn't on this computer")
	ErrChanged = errors.New("the file was changed; not touched without force")
)

// The labels a Setup's file sets on its terminal.
const (
	LabelPid     = "herdr-anywhere.chat.pid"
	LabelSession = "herdr-anywhere.chat.session"
)

// Labeled: the conversation a Setup's file named for this pane, when the process that named it is the one running.
func (p Pane) Labeled() (string, bool) {
	pid, err := strconv.Atoi(p.Labels[LabelPid])
	if err != nil || pid != p.Pid || p.Labels[LabelSession] == "" {
		return "", false
	}
	return p.Labels[LabelSession], true
}

// SetupInfo is a Setup as the phone gets it.
type SetupInfo struct {
	Agent   string     `json:"agent"`
	Name    string     `json:"name"`
	File    string     `json:"file"`             // ~ for the home folder
	Reload  string     `json:"reload,omitempty"` // what to type in an agent already running for it to use the file
	Version int        `json:"version"`
	State   SetupState `json:"state"`
}

func Info(s Setup) SetupInfo {
	return SetupInfo{Agent: s.Agent(), Name: s.Name(), File: tilde(s.File()), Reload: s.Reload(), Version: s.Version(), State: s.State()}
}

// tilde writes a path under the home folder as ~/…: what the phone shows is shorter, and shows no user name.
func tilde(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, h+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}
