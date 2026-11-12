package server

import (
	"encoding/base64"
	"errors"
	"maps"
	"net/http"
	"slices"

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
		writeJSON(w, http.StatusOK, chat.Answer{})
		return
	}
	writeJSON(w, http.StatusOK, s.cfg.Chat.Chat(r.PathValue("addr")))
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
