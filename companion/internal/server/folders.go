package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"herdr-anywhere/companion/internal/store"
)

// maxFolders caps one listing; a folder with more says "truncated".
const maxFolders = 2000

// --- /api/folders ----------------------------------------------------------
//
// The phone picks a space's folder by browsing this computer instead of typing its path from memory, and is told
// when a typed folder does not exist so it can offer to create it. herdr-core has no call that lists a folder.
// A paired phone can already run any command here, so listing folder names gives it nothing new.

// device answers 403/401 itself and returns false unless the request comes from a paired device: the token is
// in "Authorization: Bearer".
func (s *Server) device(w http.ResponseWriter, r *http.Request) bool {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "foreign origin"})
		return false
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, err := s.cfg.Store.Authenticate(token); err != nil {
		if !errors.Is(err, store.ErrUnknownToken) {
			s.log.Printf("folders: reading devices: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read the device list"})
			return false
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown device; pair again"})
		return false
	}
	return true
}

func (s *Server) home() string {
	if s.cfg.Home != "" {
		return s.cfg.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// absolute expands "~" and cleans the path; ok is false for a relative path. Empty means home.
func (s *Server) absolute(p string) (string, bool) {
	p = strings.TrimSpace(p)
	switch {
	case p == "" || p == "~":
		return s.home(), true
	case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)): // ~\ too on Windows
		p = filepath.Join(s.home(), p[2:])
	}
	if !filepath.IsAbs(p) {
		return "", false
	}
	return filepath.Clean(p), true
}

type folder struct {
	Name   string `json:"name"`
	Git    bool   `json:"git"`
	Hidden bool   `json:"hidden,omitempty"`
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	path, ok := s.absolute(r.URL.Query().Get("path"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the path must start with ~ or be a full path"})
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		st, serr := os.Stat(path)
		switch {
		case errors.Is(serr, fs.ErrNotExist):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such folder", "missing": true, "path": path, "existing": nearest(path)})
		case serr == nil && !st.IsDir():
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "not a folder", "path": path})
		default:
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "can't read this folder", "path": path})
		}
		return
	}
	// Dot folders only when the phone asks ("Show hidden", hidden=1), marked as such.
	withHidden := r.URL.Query().Get("hidden") == "1"
	list := []folder{}
	truncated := false
	for _, e := range entries {
		hidden := strings.HasPrefix(e.Name(), ".")
		if hidden && !withHidden {
			continue
		}
		// A symlink to a folder counts as one.
		if !e.IsDir() && !(e.Type()&fs.ModeSymlink != 0 && isDir(filepath.Join(path, e.Name()))) {
			continue
		}
		if len(list) == maxFolders {
			truncated = true
			break
		}
		list = append(list, folder{Name: e.Name(), Git: exists(filepath.Join(path, e.Name(), ".git")), Hidden: hidden})
	}
	sort.SliceStable(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	body := map[string]any{"path": path, "home": s.home(), "parent": filepath.Dir(path), "folders": list, "git": exists(filepath.Join(path, ".git"))}
	if truncated {
		body["truncated"] = true
	}
	writeJSON(w, http.StatusOK, body)
}

// createFolder makes the folder and any missing parents, after the phone asked the user.
func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"path\":…}"})
		return
	}
	path, ok := s.absolute(body.Path)
	if !ok || strings.TrimSpace(body.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the path must start with ~ or be a full path"})
		return
	}
	if isDir(path) {
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "created": false})
		return
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		code := http.StatusForbidden
		if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTDIR) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]any{"error": "could not create the folder: " + plainError(err), "path": path})
		return
	}
	s.log.Printf("created folder for a phone")
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "created": true})
}

// plainError drops the path from an fs error: the phone already shows it.
func plainError(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// nearest is the deepest existing folder above path.
func nearest(path string) string {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if isDir(p) || p == filepath.Dir(p) {
			return p
		}
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
