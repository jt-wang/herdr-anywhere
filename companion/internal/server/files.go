package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"herdr-anywhere/companion/internal/proc"
)

const (
	// The largest file a phone sends.
	maxUpload = 2 << 30
	// What hc takes piped in at once: it refuses more than 8 MB (hc 0.1.18, measured).
	machineChunk = 6 << 20
	// A chunk to another machine, over ssh and whatever line that has (hc's most is 600 s).
	machineChunkSeconds = 600
	// Uploads are for the agents reading them now; a phone's photos don't stay on the computer for good.
	uploadsKeep = 7 * 24 * time.Hour
)

// --- /api/files --------------------------------------------------------------
//
// A photo or a file from the phone, for an agent to read: its path goes in what the phone sends the agent. A plain HTTP
// body, read as it comes: the phone's WebSocket carries 8 MiB at most (NSH), and hc's own calls 8 MB. With `on`, it goes
// on to that machine, where the terminal is. A paired phone can already run any command here, so this gives it nothing new.

func uploadsDir(home string) string {
	return filepath.Join(home, ".cache", "herdr-anywhere", "uploads")
}

// fileName keeps a name's look and takes out what a path or a command line would trip on.
func fileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == 0 || unicode.IsControl(r):
			return -1
		case unicode.IsSpace(r) || strings.ContainsRune("'\"`$&;|<>*?()[]{}!#~", r):
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

func folderID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// pruneUploads removes folders older than uploadsKeep; only what this code made is in there.
func pruneUploads(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && e.IsDir() && time.Since(info.ModTime()) > uploadsKeep {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	name := fileName(r.URL.Query().Get("name"))
	on := r.URL.Query().Get("on")
	root := uploadsDir(s.home())
	dir := filepath.Join(root, folderID())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.Chmod(root, 0o700)
	pruneUploads(root)
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, s.maxUpload))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too large"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if on != "" {
		there, err := s.toMachine(r.Context(), on, dir, name, path)
		if err != nil {
			s.log.Printf("upload to %s: %v", on, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		path = there
	} else {
		keep = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "bytes": n})
}

// toMachine sends the file to another machine in pieces hc takes, and returns where it landed there.
func (s *Server) toMachine(ctx context.Context, on, dir, name, path string) (string, error) {
	if s.cfg.HC == "" {
		return "", errors.New("no hc on this computer")
	}
	run := func(in io.Reader, argv ...string) (string, error) {
		args := []string{"exec"}
		if in != nil {
			args = append(args, "-i")
		}
		args = append(args, "--on", on, "--timeout", fmt.Sprint(machineChunkSeconds))
		cmd := proc.CommandContext(ctx, s.cfg.HC, append(args, argv...)...)
		cmd.Stdin = in
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(errs.String()))
		}
		return out.String(), nil
	}
	there, err := run(nil, "sh", "-c", `d="$HOME/.cache/herdr-anywhere/uploads/$1"; mkdir -p "$d" && chmod 700 "$HOME/.cache/herdr-anywhere/uploads" "$d" && printf %s "$d/$2"`,
		"sh", filepath.Base(dir), name)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, s.machineChunk)
	for {
		n, rerr := io.ReadFull(f, buf)
		if n > 0 {
			if _, err := run(bytes.NewReader(buf[:n]), "sh", "-c", `cat >> "$1"`, "sh", there); err != nil {
				return "", err
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			return there, nil
		}
		if rerr != nil {
			return "", rerr
		}
	}
}
