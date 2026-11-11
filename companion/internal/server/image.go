package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// --- /api/image --------------------------------------------------------------
//
// A picture whose path a terminal shows, for the phone to show under that path: the phone taps the path and sends it
// as written, with the terminal it is in. Whether the text looks like a picture's path the phone judges (ends in
// .png, .jpg, …, at the tap); everything about the file this side judges: which file a relative path names (the
// terminal's program's folder now), whether it may be read (home, that folder, temp folders, symlinks resolved first)
// and whether it is a picture (its bytes, whatever its name says). Nothing is kept: read, sent, forgotten.

const (
	// The largest picture sent. A full-screen screenshot of a Retina display is 5 to 9 MB as PNG.
	maxImage = 12 << 20
	// A path a TUI shows; the phone sends none longer.
	maxImagePath = 1024
)

// The types sent, as http.DetectContentType names them from a file's first bytes.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (s *Server) imageTemp() []string {
	if s.cfg.ImageTemp != nil {
		return s.cfg.ImageTemp
	}
	return []string{os.TempDir(), "/tmp"}
}

// Why a picture isn't sent, as a code the phone says in its own language; msg is for logs and curl.
type imageError struct {
	code  int
	why   string
	msg   string
	limit int64 // for too_big: the most sent, in bytes
}

func (e *imageError) Error() string { return e.msg }

func refuse(code int, why, msg string) error { return &imageError{code: code, why: why, msg: msg} }

func tooBig(limit int64) error {
	return &imageError{http.StatusRequestEntityTooLarge, "too_big", "the picture is too big to send", limit}
}

func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	path, typ, data, err := s.readImage(r.URL.Query().Get("path"), r.URL.Query().Get("terminal"))
	if err != nil {
		var e *imageError
		if !errors.As(err, &e) {
			s.log.Printf("image: %v", err)
			e = &imageError{code: http.StatusInternalServerError, why: "unreadable", msg: "could not read the picture"}
		}
		reply := map[string]any{"error": e.msg, "why": e.why, "path": path}
		if e.limit > 0 {
			reply["limit"] = e.limit
		}
		writeJSON(w, e.code, reply)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Written as it is encoded: a 12 MB picture is never in memory twice over as text.
	io.WriteString(w, `{"path":`)
	p, _ := json.Marshal(path)
	w.Write(p)
	io.WriteString(w, `,"url":"data:`+typ+`;base64,`)
	enc := base64.NewEncoder(base64.StdEncoding, w)
	enc.Write(data)
	enc.Close()
	io.WriteString(w, "\"}\n")
}

// readImage answers with the full path asked for ("~" and a relative path made full, symlinks kept as shown), the
// picture's type and its bytes. Where it may read is judged on the path with its symlinks resolved.
func (s *Server) readImage(asked, terminal string) (string, string, []byte, error) {
	if err := askedPath(asked); err != nil {
		return "", "", nil, err
	}
	if machine, _, there := strings.Cut(terminal, "/"); there {
		return s.readImageThere(machine, asked, terminal)
	}
	cwd, roots, err := s.allowed(terminal)
	if err != nil {
		return "", "", nil, err
	}
	path, f, size, err := s.openImage(asked, cwd, roots)
	if err != nil {
		return path, "", nil, err
	}
	defer f.Close()
	if size > s.maxImage {
		return path, "", nil, tooBig(s.maxImage)
	}
	data, err := io.ReadAll(io.LimitReader(f, s.maxImage+1))
	if err != nil {
		return path, "", nil, err
	}
	if int64(len(data)) > s.maxImage { // it grew since
		return path, "", nil, tooBig(s.maxImage)
	}
	typ := http.DetectContentType(data)
	if !imageTypes[typ] {
		return path, "", nil, refuse(http.StatusUnsupportedMediaType, "not_image", "not a picture")
	}
	return path, typ, data, nil
}

// askedPath refuses what no terminal shows as a picture's path.
func askedPath(asked string) error {
	if asked == "" || len(asked) > maxImagePath {
		return refuse(http.StatusBadRequest, "path", "no picture's path, or a longer one than a terminal shows")
	}
	// A path a terminal shows is one line of printable text. A line break would also split what thereScript says.
	if strings.ContainsFunc(asked, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return refuse(http.StatusBadRequest, "path", "a path with a line break or a control character")
	}
	return nil
}

// allowed gives the folder a relative path is from (the terminal's program's, or "" without a terminal) and the
// folders pictures may be read from here.
func (s *Server) allowed(terminal string) (string, []string, error) {
	roots := []string{s.home()}
	roots = append(roots, s.imageTemp()...)
	if terminal == "" {
		return "", roots, nil
	}
	if !localAddress.MatchString(terminal) {
		return "", nil, refuse(http.StatusBadRequest, "path", "not a terminal's address")
	}
	cwd, err := s.terminalFolder(terminal)
	if err != nil {
		return "", nil, err
	}
	return cwd, append(roots, cwd), nil
}

// openImage opens the file a path names, if it may be read: the full path, the open file and its size.
func (s *Server) openImage(asked, cwd string, roots []string) (string, *os.File, int64, error) {
	path, ok := s.absolute(asked)
	if !ok {
		if cwd == "" {
			return "", nil, 0, refuse(http.StatusBadRequest, "relative", "a relative path needs the terminal it is in")
		}
		path = filepath.Join(cwd, asked)
	}
	real, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil, 0, refuse(http.StatusNotFound, "missing", "no such file")
	}
	if err != nil {
		return path, nil, 0, refuse(http.StatusForbidden, "unreadable", "can't read this file")
	}
	if !inside(real, roots) {
		return path, nil, 0, refuse(http.StatusForbidden, "outside", "only pictures in your home, the terminal's folder or a temp folder are shown")
	}
	f, err := os.Open(real)
	if err != nil {
		return path, nil, 0, refuse(http.StatusForbidden, "unreadable", "can't read this file")
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return path, nil, 0, refuse(http.StatusUnsupportedMediaType, "not_image", "not a picture")
	}
	return path, f, st.Size(), nil
}

// inside: is p one of roots or in one of them? Each root counts with its symlinks resolved, as p is.
func inside(p string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(root); err == nil {
			root = r
		}
		if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// localAddress: a terminal on this machine, as herdr-core names them (t7, t1b) or a name; one with "/" is on the
// machine before it (box/t3).
var localAddress = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

// terminalFolder is the folder the terminal's program is in now (`hc get`: foreground.cwd follows a cd, cwd is
// where the terminal started; hc 0.1.19, measured), else where it started.
func (s *Server) terminalFolder(addr string) (string, error) {
	if s.cfg.HC == "" {
		return "", refuse(http.StatusServiceUnavailable, "no_core", "herdr-core isn't found on this computer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, errs, err := s.runHC(ctx, "get", addr, "--json")
	if err != nil {
		if hcError(errs) == "unavailable" { // its machine is out of reach now
			return "", refuse(http.StatusBadGateway, "unreachable", "couldn't reach the terminal's machine")
		}
		return "", refuse(http.StatusNotFound, "no_terminal", "no such terminal")
	}
	var t struct {
		Cwd        string `json:"cwd"`
		Foreground *struct {
			Cwd string `json:"cwd"`
		} `json:"foreground"`
	}
	if json.Unmarshal(out, &t) != nil {
		return "", refuse(http.StatusNotFound, "no_terminal", "no such terminal")
	}
	cwd := t.Cwd
	if t.Foreground != nil && t.Foreground.Cwd != "" {
		cwd = t.Foreground.Cwd
	}
	if !filepath.IsAbs(cwd) {
		return "", refuse(http.StatusNotFound, "no_terminal", "herdr-core doesn't say where this terminal is")
	}
	return filepath.Clean(cwd), nil
}

// --- a picture on another machine ---------------------------------------------
//
// A terminal on another machine (box/t3) shows paths on that machine. The same rules hold there, judged there, as
// only that machine knows its home, its symlinks and its files: thereScript runs in its sh through `hc exec --on`,
// which goes over the connection hc keeps to it (no new ssh connection a picture; measured with hc 0.1.19). It says
// by its exit code why a file isn't sent, or writes the file. What a program there writes, hc carries at most 8 MB of
// (machine.exec, "the first 8 MB"), so that is the most sent from there. Whether it is a picture is judged here, on
// its bytes, as for a file here. A machine without sh (Windows) says "remote".

// The most hc brings back from a program on another machine.
const maxThere = 8 << 20

// thereLook is the rules, in sh, that thereScript and thereCheck share. Set before it: max, cwd, and temps (the temp
// folders allowed, one a line; empty: /tmp and $TMPDIR). look PATH sets p (the full path, symlinks kept), r (the file
// it is, symlinks resolved) and size, or returns 10 none, 11 outside, 12 not a file, 13 too big (r and size set), 14
// unreadable. PATH as the terminal shows it: "~" is that machine's home, a relative path is from cwd.
const thereLook = `nl='
'
[ -n "$temps" ] || temps=/tmp$nl${TMPDIR:-}
roots=
set -f; IFS=$nl
for root in $HOME$nl$cwd$nl$temps; do
  root=$(cd -P -- "$root" 2>/dev/null && pwd -P) || continue
  roots=$roots$root$nl
done
unset IFS; set +f
look() {
  case $1 in
  "~") p=$HOME ;;
  "~/"*) p=$HOME/${1#"~/"} ;;
  /*) p=$1 ;;
  *) p=$cwd/$1 ;;
  esac
  r= size=
  [ -e "$p" ] || return 10
  q=$p n=0
  while [ -L "$q" ]; do
    n=$((n + 1)); [ $n -le 40 ] || return 14
    l=$(readlink "$q") || return 14
    case $l in /*) q=$l ;; *) q=${q%/*}/$l ;; esac
  done
  d=$(cd -P -- "${q%/*}/" 2>/dev/null && pwd -P) || return 14
  b=${q##*/}
  case $b in .|..|"") return 12 ;; esac
  r=${d%/}/$b
  in=0
  set -f; IFS=$nl
  for root in $roots; do
    case $root in /) in=1; break ;; esac
    case $r in "$root"|"$root"/*) in=1; break ;; esac
  done
  unset IFS; set +f
  [ $in = 1 ] || return 11
  [ -f "$r" ] || return 12
  [ -r "$r" ] || return 14
  size=$(wc -c < "$r" | tr -d ' ') || return 14
  [ "$size" -le "$max" ] || return 13
}
`

// thereScript: sh -c thereScript sh MAX PATH FOLDER TEMPS. TEMPS: the temp folders allowed, one a line. Writes "path
// P" to stderr first, then the file's bytes to stdout and "size N" to stderr, or exits with look's code.
const thereScript = `max=$1 asked=$2 cwd=$3 temps=$4
` + thereLook + `look "$asked"; c=$?
printf 'path %s\n' "$p" >&2
[ $c = 0 ] || exit $c
printf 'size %s\n' "$size" >&2
exec cat -- "$r"
`

// thereCheck: sh -c thereCheck sh MAX FOLDER TEMPS PATH…. For each PATH, in order, writes a line "is CODE HEX" to
// stdout: CODE look's, HEX the file's first 16 bytes for 0 and 13 (enough to say whether it is a picture).
const thereCheck = `max=$1 cwd=$2 temps=$3; shift 3
` + thereLook + `for a in "$@"; do
  look "$a"; c=$? h=
  case $c in 0|13) h=$(dd if="$r" bs=16 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n') ;; esac
  printf 'is %s %s\n' "$c" "$h"
done
`

var thereRefusals = map[int]*imageError{
	10: {code: http.StatusNotFound, why: "missing", msg: "no such file"},
	11: {code: http.StatusForbidden, why: "outside", msg: "only pictures in your home, the terminal's folder or a temp folder are shown"},
	12: {code: http.StatusUnsupportedMediaType, why: "not_image", msg: "not a picture"},
	14: {code: http.StatusForbidden, why: "unreadable", msg: "can't read this file"},
}

func (s *Server) readImageThere(machine, asked, terminal string) (string, string, []byte, error) {
	cwd, err := s.terminalFolder(terminal)
	if err != nil {
		return "", "", nil, err
	}
	max := min(s.maxImage, maxThere)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The path goes as an argument of its own, never into the script's text: hc sends argv to that machine as a JSON
	// array and its core starts argv[0] with the rest as arguments, no shell between (herdr-core v0.1.19,
	// crates/herdr-core/src/remote.rs `route`/`call_into`, exec.rs `run`). The sh there reads it as "$2".
	args := []string{"exec", "--on", machine, "--json", "--timeout", "30", "--", "sh", "-c", thereScript, "sh",
		fmt.Sprint(max), asked, cwd, strings.Join(s.remoteTemp, "\n")}
	res, err := s.runThere(ctx, machine, args)
	if err != nil {
		return "", "", nil, err
	}
	path, size := "", int64(-1)
	for _, l := range strings.Split(string(res.Stderr), "\n") {
		if p, ok := strings.CutPrefix(l, "path "); ok && path == "" {
			path = pathpkg.Clean(p) // as filepath.Join makes one here: lexically, symlinks kept
		} else if n, ok := strings.CutPrefix(l, "size "); ok {
			fmt.Sscan(n, &size)
		}
	}
	if path == "" { // its sh never ran the script (no sh there)
		return "", "", nil, refuse(http.StatusBadRequest, "remote", "pictures on this machine can't be read")
	}
	if *res.Code == 13 {
		return path, "", nil, tooBig(max)
	}
	if e, ok := thereRefusals[*res.Code]; ok {
		return path, "", nil, e
	}
	if *res.Code != 0 || size < 0 {
		return path, "", nil, refuse(http.StatusForbidden, "unreadable", "can't read this file")
	}
	if res.Truncated || int64(len(res.Stdout)) != size || size > max { // it grew since
		return path, "", nil, tooBig(max)
	}
	typ := http.DetectContentType(res.Stdout)
	if !imageTypes[typ] {
		return path, "", nil, refuse(http.StatusUnsupportedMediaType, "not_image", "not a picture")
	}
	return path, typ, res.Stdout, nil
}

// --- /api/image/check ----------------------------------------------------------
//
// Which of the paths a terminal shows are pictures, for the phone to underline them and open the sheet on a tap only
// for those: POST {terminal, paths} answers {paths: [{asked, path, picture, why}]} in the same order. The same rules
// as /api/image; a file's first bytes say whether it is a picture, nothing more is read. A picture too big to send
// is a picture (its sheet says it is too big). One call to the terminal's machine for all the paths.

// The most paths a check asks about: more than the rows of a phone's screen show.
const maxCheck = 64

type checked struct {
	Asked   string `json:"asked"`
	Path    string `json:"path,omitempty"`
	Picture bool   `json:"picture"`
	Why     string `json:"why,omitempty"`
}

func (s *Server) imageCheck(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	var q struct {
		Terminal string   `json:"terminal"`
		Paths    []string `json:"paths"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&q) != nil || len(q.Paths) > maxCheck {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("a terminal and at most %d paths", maxCheck), "why": "path"})
		return
	}
	var out []checked
	var err error
	if machine, _, there := strings.Cut(q.Terminal, "/"); there {
		out, err = s.checkThere(machine, q.Terminal, q.Paths)
	} else {
		out, err = s.checkHere(q.Terminal, q.Paths)
	}
	if err != nil {
		var e *imageError
		if !errors.As(err, &e) {
			s.log.Printf("image check: %v", err)
			e = &imageError{code: http.StatusInternalServerError, why: "unreadable", msg: "could not check"}
		}
		writeJSON(w, e.code, map[string]any{"error": e.msg, "why": e.why})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": out})
}

func whyOf(err error) string {
	var e *imageError
	if errors.As(err, &e) {
		return e.why
	}
	return "unreadable"
}

func (s *Server) checkHere(terminal string, paths []string) ([]checked, error) {
	cwd, roots, err := s.allowed(terminal)
	if err != nil {
		return nil, err
	}
	out := make([]checked, len(paths))
	for i, asked := range paths {
		out[i] = checked{Asked: asked}
		if err := askedPath(asked); err != nil {
			out[i].Why = whyOf(err)
			continue
		}
		path, f, size, err := s.openImage(asked, cwd, roots)
		out[i].Path = path
		if err != nil {
			out[i].Why = whyOf(err)
			continue
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		f.Close()
		if !imageTypes[http.DetectContentType(head[:n])] {
			out[i].Why = "not_image"
			continue
		}
		out[i].Picture = true
		if size > s.maxImage {
			out[i].Why = "too_big"
		}
	}
	return out, nil
}

func (s *Server) checkThere(machine, terminal string, paths []string) ([]checked, error) {
	cwd, err := s.terminalFolder(terminal)
	if err != nil {
		return nil, err
	}
	out := make([]checked, len(paths))
	var ask []string // the paths that go there, as they may
	var at []int
	for i, asked := range paths {
		out[i] = checked{Asked: asked}
		if err := askedPath(asked); err != nil {
			out[i].Why = whyOf(err)
			continue
		}
		ask, at = append(ask, asked), append(at, i)
	}
	if len(ask) == 0 {
		return out, nil
	}
	max := min(s.maxImage, maxThere)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// As readImageThere: each path an argument of its own, never in the script's text.
	args := append([]string{"exec", "--on", machine, "--json", "--timeout", "30", "--", "sh", "-c", thereCheck, "sh",
		fmt.Sprint(max), cwd, strings.Join(s.remoteTemp, "\n")}, ask...)
	res, err := s.runThere(ctx, machine, args)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
	if *res.Code != 0 || len(lines) != len(ask) {
		return nil, refuse(http.StatusBadRequest, "remote", "pictures on this machine can't be read")
	}
	for j, l := range lines {
		o := &out[at[j]]
		var code int
		var hex string
		if n, _ := fmt.Sscanf(l, "is %d %s", &code, &hex); n < 1 {
			return nil, refuse(http.StatusBadRequest, "remote", "pictures on this machine can't be read")
		}
		switch e, refused := thereRefusals[code]; {
		case refused:
			o.Why = e.why
		case code != 0 && code != 13:
			o.Why = "unreadable"
		case !imageTypes[http.DetectContentType(unhex(hex))]:
			o.Why = "not_image"
		default:
			o.Picture = true
			if code == 13 {
				o.Why = "too_big"
			}
		}
	}
	return out, nil
}

func unhex(h string) []byte {
	b := make([]byte, len(h)/2)
	for i := range b {
		fmt.Sscanf(h[2*i:2*i+2], "%02x", &b[i])
	}
	return b
}

// What `hc exec --json` says a program on another machine did.
type execResult struct {
	Code      *int   `json:"code"`
	Stdout    []byte `json:"stdout"` // base64
	Stderr    []byte `json:"stderr"`
	Truncated bool   `json:"truncated"`
	TimedOut  bool   `json:"timed_out"`
}

// runThere runs `hc exec --on machine` (args from "exec" on) and gives what the program did there.
func (s *Server) runThere(ctx context.Context, machine string, args []string) (*execResult, error) {
	out, errs, err := s.runHC(ctx, args...)
	if err != nil && hcError(errs) == "spawn_failed" { // no sh there
		return nil, refuse(http.StatusBadRequest, "remote", "pictures on this machine can't be read")
	}
	var res execResult
	if err != nil || json.Unmarshal(out, &res) != nil || res.Code == nil || res.TimedOut {
		return nil, refuse(http.StatusBadGateway, "unreachable", "couldn't reach "+machine)
	}
	return &res, nil
}

// runHC runs hc and gives what it wrote to stdout and to stderr.
func (s *Server) runHC(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, s.cfg.HC, args...)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	return out.Bytes(), errs.Bytes(), err
}

// hcError is the kind of error hc said: {"error":{"kind":…}} on stderr (hc 0.1.19, measured), or "".
func hcError(stderr []byte) string {
	var e struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(stderr), &e)
	return e.Error.Kind
}
