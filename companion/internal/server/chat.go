package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"herdr-anywhere/companion/internal/chat"
)

// --- /api/chat ---------------------------------------------------------------------------------------------
//
// The phone's chat view: an agent's turns from its own session record (internal/chat). Only reads; what the phone
// types still goes into the terminal. The path holds the terminal's address only, never text, since the request log
// records paths.

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	if s.cfg.Chat == nil {
		writeChat(w, r, chat.Answer{})
		return
	}
	a, err := s.cfg.Chat.ChatPage(r.PathValue("addr"), r.URL.Query().Get("before"))
	switch {
	case errors.Is(err, chat.ErrBadCursor):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid-cursor"})
	case errors.Is(err, chat.ErrCursor):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "history-changed"})
	case errors.Is(err, chat.ErrHistoryLimit):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "history-limit"})
	case err != nil:
		writeJSON(w, http.StatusConflict, map[string]string{"error": "history-unavailable"})
	default:
		writeChat(w, r, a)
	}
}

// Validate only after authentication and ChatPage: even unchanged turns may now belong to another session/page,
// or discovery may have failed. Hash the complete JSON bytes we send, not the reader's turns alone. This saves
// response traffic, not reading/parsing the agent's record.
func writeChat(w http.ResponseWriter, r *http.Request, a chat.Answer) {
	var body bytes.Buffer
	_ = json.NewEncoder(&body).Encode(a)
	tag := fmt.Sprintf(`"%x"`, sha256.Sum256(body.Bytes()))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Method == http.MethodGet {
		for _, line := range r.Header.Values("If-None-Match") {
			// The wildcard is an alternative to a tag list, not a star inside an opaque quoted tag.
			if strings.TrimSpace(line) == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			for _, candidate := range strings.Split(line, ",") {
				candidate = strings.TrimSpace(candidate)
				if strings.TrimPrefix(candidate, "W/") == tag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body.Bytes())
}

func (s *Server) chatStep(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	if s.cfg.Chat == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such step"})
		return
	}
	d, err := s.cfg.Chat.Step(r.PathValue("addr"), r.PathValue("step"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such step"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// chatImage sends an image as a data: URL, which the app's Image shows without a request of its own (it could not
// send the device's token).
func (s *Server) chatImage(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	if s.cfg.Chat == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such image"})
		return
	}
	media, data, err := s.cfg.Chat.Image(r.PathValue("addr"), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such image"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "data:" + media + ";base64," + base64.StdEncoding.EncodeToString(data)})
}

// --- /api/chat-setups --------------------------------------------------------------------------------------
//
// Some agents' chats need a small file in the agent's own folder (chat.Setup). The phone lists them, asks once, and
// offers Install (PUT) and Remove (DELETE) in the chat and in its settings. A file someone changed is left as it is
// (409) unless the phone asks again with ?force=1; without the agent on the computer nothing is made (409 no-agent).

func (s *Server) chatSetups(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	list := []chat.SetupInfo{}
	for _, k := range slices.Sorted(maps.Keys(s.cfg.ChatSetups)) {
		list = append(list, chat.Info(s.cfg.ChatSetups[k]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"setups": list})
}

func (s *Server) chatSetupSet(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	st, ok := s.cfg.ChatSetups[r.PathValue("agent")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such setup"})
		return
	}
	force := r.URL.Query().Get("force") == "1"
	var err error
	if r.Method == http.MethodPut {
		err = st.Install(force)
	} else {
		err = st.Remove(force)
	}
	info := chat.Info(st)
	switch {
	case errors.Is(err, chat.ErrChanged), errors.Is(err, chat.ErrNoAgent):
		writeJSON(w, http.StatusConflict, info)
	case err != nil:
		s.log.Printf("chat setup %s: %v", info.Agent, err)
		writeJSON(w, http.StatusInternalServerError, info)
	default:
		writeJSON(w, http.StatusOK, info)
	}
}
