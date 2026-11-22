package track

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/pi"
)

// PaneReport says what one pane did for one thing, read from the agent's own
// record and from git, with no model call.
type PaneReport struct {
	Thing   string   `json:"thing"`
	Outcome string   `json:"outcome"`
	Pane    string   `json:"pane"`
	Agent   string   `json:"agent,omitempty"`
	Status  string   `json:"status,omitempty"`
	Turns   int      `json:"turns_since"` // hc: turns since the hand-off; -1 when unknown
	Msg     string   `json:"msg,omitempty"`
	Reply   string   `json:"reply,omitempty"` // the last finished reply, cut to 300 characters
	Source  string   `json:"source,omitempty"`
	Commits []string `json:"commits,omitempty"`
	Dirty   int      `json:"dirty"` // uncommitted changed files in its folder
	Note    string   `json:"note,omitempty"`
}

type HCDetail struct {
	Pid    int
	App    string
	Msg    string
	Labels map[string]string
}

// ReportSources are the places Report reads; zero fields use this Mac's.
type ReportSources struct {
	ClaudeHome string // ~/.claude
	PiHome     string // ~/.pi/agent
	HC         string // hc binary for the default HCGet
	HCGet      func(addr string) (HCDetail, error)
	Git        func(cwd string, since time.Time) (commits []string, dirty int, err error)
}

func (s *ReportSources) defaults() {
	home, _ := os.UserHomeDir()
	if s.ClaudeHome == "" {
		s.ClaudeHome = filepath.Join(home, ".claude")
	}
	if s.PiHome == "" {
		s.PiHome = filepath.Join(home, ".pi", "agent")
	}
	if s.HCGet == nil {
		hc := s.HC
		if hc == "" {
			hc = "hc"
		}
		s.HCGet = func(addr string) (HCDetail, error) { return hcGet(hc, addr) }
	}
	if s.Git == nil {
		s.Git = gitSince
	}
}

// Report covers every pane of every active thing, or of thing id only.
func Report(f File, live Live, src ReportSources, now time.Time, since time.Duration, id string) []PaneReport {
	src.defaults()
	var out []PaneReport
	for _, t := range f.Things {
		if id != "" && t.ID != id {
			continue
		}
		if id == "" && (t.Stage == Done || t.Stage == Dropped || t.Stage == Parked) {
			continue
		}
		for _, p := range t.Panes {
			out = append(out, reportPane(t, p, live, src, now, since))
		}
	}
	return out
}

func reportPane(t Thing, p PaneRef, live Live, src ReportSources, now time.Time, since time.Duration) PaneReport {
	lp := live.pane(p)
	r := PaneReport{Thing: t.ID, Outcome: t.Outcome, Pane: labelOf(PaneRow{Ref: p, ID: lp.id, Name: lp.name}), Status: lp.status, Turns: -1}
	if !lp.Found {
		r.Note = "pane " + lp.state
		return r
	}
	if p.TurnAt != nil && p.Side == "hc" {
		r.Turns = lp.counter - *p.TurnAt
	}
	from := now.Add(-since)
	if p.At > 0 {
		from = time.UnixMilli(p.At)
	}
	if lp.cwd != "" {
		if commits, dirty, err := src.Git(lp.cwd, from); err == nil {
			r.Commits, r.Dirty = commits, dirty
		}
	}
	switch p.Side {
	case "hc":
		d, err := src.HCGet(lp.id)
		if err != nil {
			r.Note = "hc get: " + err.Error()
			return r
		}
		r.Agent, r.Msg = d.App, d.Msg
		switch d.App {
		case "claude-code":
			r.Reply, r.Source, r.Note = claudeByPid(src.ClaudeHome, d.Pid)
		case "pi":
			r.Reply, r.Source, r.Note = piByCwd(src.PiHome, lp.cwd)
		case "omp":
			r.Reply, r.Source, r.Note = ompByPane(d, src.HC)
		default:
			r.Note = "no reader for " + d.App
		}
	case "herdr":
		a := live.herdrAgent(p)
		r.Agent = a.Agent
		switch {
		case a.Agent == "claude" && a.Session != "":
			r.Reply, r.Source, r.Note = claudeBySession(src.ClaudeHome, a.Session)
		case a.Agent == "pi":
			r.Reply, r.Source, r.Note = piByCwd(src.PiHome, lp.cwd)
		default:
			r.Note = "no session id from old Herdr"
		}
	}
	r.Reply = cut(r.Reply, 300)
	return r
}

func ompByPane(p HCDetail, hc string) (reply, source, note string) {
	r := &pi.Reader{Setup: pi.OMPSetup(hc)}
	file, _, err := r.FindPane(chat.Pane{Pid: p.Pid, Labels: p.Labels})
	if err != nil {
		return "", "", err.Error()
	}
	turns, err := r.Turns(file)
	if err != nil {
		return "", "", err.Error()
	}
	source = "omp session (labeled)"
	if len(turns) == 0 || turns[len(turns)-1].End != "done" || turns[len(turns)-1].Reply == "" {
		return "", source, "the last turn has no finished reply"
	}
	return turns[len(turns)-1].Reply, source, ""
}

func (l Live) herdrAgent(p PaneRef) HerdrAgent {
	for _, a := range l.Herdr {
		if (p.PaneID != "" && a.PaneID == p.PaneID) || (p.PaneID == "" && p.Name != "" && a.Name == p.Name) {
			return a
		}
	}
	return HerdrAgent{}
}

func claudeByPid(home string, pid int) (reply, source, note string) {
	b, err := os.ReadFile(filepath.Join(home, "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return "", "", "no Claude session file for pid " + strconv.Itoa(pid)
	}
	var s struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(b, &s) != nil || s.SessionID == "" {
		return "", "", "Claude session file has no sessionId"
	}
	return claudeBySession(home, s.SessionID)
}

func claudeBySession(home, sid string) (reply, source, note string) {
	m, _ := filepath.Glob(filepath.Join(home, "projects", "*", sid+".jsonl"))
	if len(m) != 1 {
		return "", "", fmt.Sprintf("%d transcripts for session %s", len(m), sid)
	}
	reply, err := claudeLastReply(m[0])
	if err != nil {
		return "", "", err.Error()
	}
	if reply == "" {
		return "", "claude transcript", "the last turn has no finished reply"
	}
	return reply, "claude transcript", ""
}

// claudeLastReply is the end_turn text after the last real prompt. A turn
// that didn't finish gives "", never the previous turn's reply.
func claudeLastReply(path string) (string, error) {
	type block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var reply []string
	lastID := ""
	err := eachLine(path, func(b []byte) {
		var l struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Compact bool   `json:"isCompactSummary"`
			Message struct {
				ID         string          `json:"id"`
				Role       string          `json:"role"`
				StopReason string          `json:"stop_reason"`
				Content    json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(b, &l) != nil {
			return
		}
		switch l.Type {
		case "user":
			if l.IsMeta || l.Compact {
				return
			}
			var s string
			if json.Unmarshal(l.Message.Content, &s) == nil {
				reply, lastID = nil, ""
				return
			}
			var bs []block
			if json.Unmarshal(l.Message.Content, &bs) == nil {
				for _, x := range bs {
					if x.Type == "text" {
						reply, lastID = nil, ""
						return
					}
				}
			}
		case "assistant":
			if l.Message.StopReason != "end_turn" {
				return
			}
			var bs []block
			if json.Unmarshal(l.Message.Content, &bs) != nil {
				return
			}
			if l.Message.ID != lastID {
				reply, lastID = nil, l.Message.ID
			}
			for _, x := range bs {
				if x.Type == "text" && strings.TrimSpace(x.Text) != "" {
					reply = append(reply, x.Text)
				}
			}
		}
	})
	return strings.TrimSpace(strings.Join(reply, "\n")), err
}

func piDirName(cwd string) string {
	return "--" + strings.ReplaceAll(strings.TrimPrefix(cwd, "/"), "/", "-") + "--"
}

// piByCwd reads the newest Pi session in that folder: Pi has no pid map, so
// this is a time match and says so.
func piByCwd(home, cwd string) (reply, source, note string) {
	dirs := []string{filepath.Join(home, "sessions", piDirName(cwd))}
	if r, err := filepath.EvalSymlinks(cwd); err == nil && r != cwd {
		dirs = append(dirs, filepath.Join(home, "sessions", piDirName(r)))
	}
	var files []string
	for _, d := range dirs {
		m, _ := filepath.Glob(filepath.Join(d, "*.jsonl"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		return "", "", "no Pi session in " + cwd
	}
	sort.Slice(files, func(i, j int) bool { return mtime(files[i]).After(mtime(files[j])) })
	reply, err := piLastReply(files[0])
	if err != nil {
		return "", "", err.Error()
	}
	src := "pi session (newest in folder)"
	if len(files) > 1 {
		src = fmt.Sprintf("pi session (newest of %d in folder)", len(files))
	}
	if reply == "" {
		return "", src, "the last turn has no finished reply"
	}
	return reply, src, ""
}

func mtime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func piLastReply(path string) (string, error) {
	var reply string
	err := eachLine(path, func(b []byte) {
		var l struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(b, &l) != nil || l.Type != "message" {
			return
		}
		switch l.Message.Role {
		case "user":
			reply = ""
		case "assistant":
			if l.Message.StopReason != "stop" {
				return
			}
			var parts []string
			for _, c := range l.Message.Content {
				if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
					parts = append(parts, c.Text)
				}
			}
			reply = strings.Join(parts, "\n")
		}
	})
	return strings.TrimSpace(reply), err
}

// eachLine reads JSONL with no line-length limit (Claude lines reach 1.6 MB).
func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			fn(b)
		}
		if err != nil {
			return nil
		}
	}
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func hcGet(hc, addr string) (HCDetail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, hc, "get", addr, "--json").Output()
	if err != nil {
		return HCDetail{}, err
	}
	var v struct {
		Labels     map[string]string `json:"labels"`
		Foreground struct {
			Pid int `json:"pid"`
		} `json:"foreground"`
		Status *struct {
			App string `json:"app"`
			Msg string `json:"msg"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return HCDetail{}, err
	}
	d := HCDetail{Pid: v.Foreground.Pid, Labels: v.Labels}
	if v.Status != nil {
		d.App, d.Msg = v.Status.App, v.Status.Msg
	}
	return d, nil
}

func gitSince(cwd string, since time.Time) ([]string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "log", "--since=@"+strconv.FormatInt(since.Unix(), 10), "--format=%h %s").Output()
	if err != nil {
		return nil, 0, err
	}
	var commits []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			commits = append(commits, l)
		}
	}
	st, err := exec.CommandContext(ctx, "git", "-C", cwd, "status", "--porcelain").Output()
	if err != nil {
		return commits, 0, nil
	}
	dirty := 0
	for _, l := range strings.Split(string(st), "\n") {
		if strings.TrimSpace(l) != "" {
			dirty++
		}
	}
	return commits, dirty, nil
}

func ReportText(reps []PaneReport) string {
	var b strings.Builder
	for _, r := range reps {
		fmt.Fprintf(&b, "%s  %s\n  %s", r.Thing, r.Outcome, r.Pane)
		if r.Agent != "" {
			fmt.Fprintf(&b, " [%s]", r.Agent)
		}
		if r.Status != "" {
			fmt.Fprintf(&b, " %s", r.Status)
		}
		if r.Turns >= 0 {
			fmt.Fprintf(&b, ", %d turns since the hand-off", r.Turns)
		}
		b.WriteString("\n")
		if r.Reply != "" {
			fmt.Fprintf(&b, "    reply: %s\n", oneLine(r.Reply))
		} else if r.Msg != "" {
			fmt.Fprintf(&b, "    7501: %s\n", r.Msg)
		}
		for _, c := range r.Commits {
			fmt.Fprintf(&b, "    commit: %s\n", c)
		}
		if r.Dirty > 0 {
			fmt.Fprintf(&b, "    uncommitted: %d files\n", r.Dirty)
		}
		if r.Note != "" {
			fmt.Fprintf(&b, "    (%s)\n", r.Note)
		}
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
