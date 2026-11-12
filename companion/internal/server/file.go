package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- /api/file ----------------------------------------------------------------
//
// A file a chat reply or step names (a rendered video, a PDF, notes), for the phone to show or open: POST
// /api/file/check says what each path is, GET /api/file sends one, in parts when asked (Range), as a video player
// reads it. The same rules as a picture's say where it may be read: home, the terminal's program's folder, temp
// folders, symlinks resolved first. A file on another machine is not sent yet: the check says "remote".

// movieTypes: what an ISO media file (an "ftyp" box first) is by its name, when its bytes don't say (Go's sniffing
// knows MP4 by its brand, not QuickTime's "qt  ").
var movieTypes = map[string]string{".mov": "video/quicktime", ".mp4": "video/mp4", ".m4v": "video/mp4"}

// fileKind: what a file is, as the phone shows it, and the type it is sent with: from its first bytes, else for a
// movie its name.
func fileKind(head []byte, name string) (kind, typ string) {
	typ = http.DetectContentType(head)
	if t, ok := movieTypes[strings.ToLower(filepath.Ext(name))]; ok && !strings.HasPrefix(typ, "video/") && len(head) >= 8 && string(head[4:8]) == "ftyp" {
		typ = t
	}
	switch {
	case strings.HasPrefix(typ, "video/"):
		return "video", typ
	case typ == "application/pdf":
		return "pdf", typ
	case imageTypes[typ]:
		return "image", typ
	case strings.HasPrefix(typ, "text/"):
		// HTML and XML go as plain text: the phone shows their source and never runs them.
		return "text", "text/plain; charset=utf-8"
	}
	return "other", "application/octet-stream"
}

// openFile opens the file a path names, if it may be read: the full path asked for and the open file.
func (s *Server) openFile(asked, terminal string) (string, *os.File, os.FileInfo, error) {
	if err := askedPath(asked); err != nil {
		return "", nil, nil, err
	}
	if _, _, there := strings.Cut(terminal, "/"); there {
		return "", nil, nil, refuse(http.StatusNotImplemented, "remote", "files on another machine are not sent yet")
	}
	cwd, roots, err := s.allowed(terminal)
	if err != nil {
		return "", nil, nil, err
	}
	path, ok := s.absolute(asked)
	if !ok {
		if cwd == "" {
			return "", nil, nil, refuse(http.StatusBadRequest, "relative", "a relative path needs the terminal it is in")
		}
		path = filepath.Join(cwd, asked)
	}
	real, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil, nil, refuse(http.StatusNotFound, "missing", "no such file")
	}
	if err != nil {
		return path, nil, nil, refuse(http.StatusForbidden, "unreadable", "can't read this file")
	}
	if !inside(real, roots) {
		return path, nil, nil, refuse(http.StatusForbidden, "outside", "only files in your home, the terminal's folder or a temp folder are sent")
	}
	f, err := os.Open(real)
	if err != nil {
		return path, nil, nil, refuse(http.StatusForbidden, "unreadable", "can't read this file")
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return path, nil, nil, refuse(http.StatusUnsupportedMediaType, "not_file", "not a file")
	}
	return path, f, st, nil
}

func head(f *os.File) []byte {
	b := make([]byte, 512)
	n, _ := f.ReadAt(b, 0)
	return b[:n]
}

// fileLink is what a link opens: one path as a terminal shows it, for the device that asked, until it is unused
// for fileLinkIdle.
type fileLink struct {
	path, terminal, token string
	used                  time.Time
}

const defaultFileLinkIdle = 15 * time.Minute

// fileLinkNew makes a link to one file for a player that can't send the device's token: the phone passes it to the
// video player, which reads the file in parts as it plays.
func (s *Server) fileLinkNew(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	var q struct {
		Terminal string `json:"terminal"`
		Path     string `json:"path"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&q) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "a terminal and a path", "why": "path"})
		return
	}
	_, f, _, err := s.openFile(q.Path, q.Terminal)
	if err != nil {
		e := &imageError{code: http.StatusInternalServerError, why: "unreadable", msg: "could not read the file"}
		errors.As(err, &e)
		writeJSON(w, e.code, map[string]any{"error": e.msg, "why": e.why})
		return
	}
	f.Close()
	b := make([]byte, 24)
	rand.Read(b)
	key := base64.RawURLEncoding.EncodeToString(b)
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	if s.fileLinks == nil {
		s.fileLinks = map[string]*fileLink{}
	}
	idle := s.linkIdle()
	for k, l := range s.fileLinks { // the old ones go as new ones come
		if time.Since(l.used) > idle {
			delete(s.fileLinks, k)
		}
	}
	s.fileLinks[key] = &fileLink{path: q.Path, terminal: q.Terminal, token: token, used: time.Now()}
	s.mu.Unlock()
	u := "/api/file?" + url.Values{"path": {q.Path}, "terminal": {q.Terminal}, "key": {key}}.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"url": u})
}

func (s *Server) linkIdle() time.Duration {
	if s.fileLinkIdle > 0 {
		return s.fileLinkIdle
	}
	return defaultFileLinkIdle
}

// linked: does the request carry a link to this very file, used lately, of a device still paired?
func (s *Server) linked(r *http.Request) bool {
	key := r.URL.Query().Get("key")
	if key == "" || !sameOrigin(r) {
		return false
	}
	s.mu.Lock()
	l := s.fileLinks[key]
	ok := l != nil && l.path == r.URL.Query().Get("path") && l.terminal == r.URL.Query().Get("terminal") && time.Since(l.used) <= s.linkIdle()
	token := ""
	if ok {
		token = l.token
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	if _, err := s.cfg.Store.Authenticate(token); err != nil {
		return false
	}
	s.mu.Lock()
	l.used = time.Now()
	s.mu.Unlock()
	return true
}

// file sends one file, in parts when asked: to the device, or to a player by a link (fileLinkNew).
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("key") != "" {
		if !s.linked(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "this link has ended; open the file again"})
			return
		}
	} else if !s.device(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	path, f, st, err := s.openFile(r.URL.Query().Get("path"), r.URL.Query().Get("terminal"))
	if err != nil {
		var e *imageError
		if !errors.As(err, &e) {
			s.log.Printf("file: %v", err)
			e = &imageError{code: http.StatusInternalServerError, why: "unreadable", msg: "could not read the file"}
		}
		writeJSON(w, e.code, map[string]any{"error": e.msg, "why": e.why, "path": path})
		return
	}
	defer f.Close()
	_, typ := fileKind(head(f), st.Name())
	w.Header().Set("Content-Type", typ)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// A file a reply names, as the check says it: what it is, its name and size, or why it can't be opened.
type fileChecked struct {
	Asked string `json:"asked"`
	Path  string `json:"path,omitempty"`
	Name  string `json:"name,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Why   string `json:"why,omitempty"`
}

func (s *Server) fileCheck(w http.ResponseWriter, r *http.Request) {
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
	if !strings.Contains(q.Terminal, "/") {
		if _, _, err := s.allowed(q.Terminal); err != nil {
			e := &imageError{code: http.StatusInternalServerError, why: "unreadable", msg: "could not check"}
			errors.As(err, &e)
			writeJSON(w, e.code, map[string]any{"error": e.msg, "why": e.why})
			return
		}
	}
	out := make([]fileChecked, len(q.Paths))
	for i, asked := range q.Paths {
		out[i] = fileChecked{Asked: asked}
		path, f, st, err := s.openFile(asked, q.Terminal)
		if err != nil {
			out[i].Why = whyOf(err)
			continue
		}
		out[i].Path, out[i].Name, out[i].Size = path, filepath.Base(path), st.Size()
		out[i].Kind, _ = fileKind(head(f), st.Name())
		f.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}
