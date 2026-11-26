package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/proc"
)

// --- /api/cli ----------------------------------------------------------------
//
// The progress page's commands (app/src/model/progress.ts): `herdr-anywhere progress …` and `track tree`, run as
// this companion's own program with the arguments the phone sent, no shell between. The phone ran them through
// herdr-core's machine.exec as a POSIX shell line before, which Windows' cmd.exe cannot read. The program works on
// this companion's state folder and its herdr-core, not the defaults: a companion started with --state or --hc is
// the one the phone is paired with. It writes UTF-8 (where it repeats another program's words, it converted them
// when it read them), so what it writes comes back as it is.

// cliOK: the phone may run only the commands the progress page sends, word for word, with an agent id and a
// language tag as the only values it chooses. No flag such as --hc, --state or --cmd gets through: each would let
// the phone pick a program, a file or a command line for this computer to use.
func cliOK(a []string) bool {
	switch len(a) {
	case 2:
		return a[0] == "progress" && (a[1] == "on" || a[1] == "off" || a[1] == "agents")
	case 3:
		return a[0] == "progress" && a[1] == "lang" && cliLang.MatchString(a[2])
	case 5:
		return a[0] == "track" && a[1] == "tree" && a[2] == "--json" && a[3] == "--herdr" && a[4] == "-"
	case 6:
		return a[0] == "progress" && a[1] == "organizer" && a[2] == "--agent" && cliAgent.MatchString(a[3]) &&
			a[4] == "--lang" && cliLang.MatchString(a[5])
	}
	return false
}

var (
	// cliAgent: an agent id as `progress agents` lists them, such as claude-code.
	cliAgent = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// cliLang: a BCP 47 tag, as `progress organizer --lang` takes it.
	cliLang = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

const cliTimeout = 30 * time.Second

func (s *Server) cli(w http.ResponseWriter, r *http.Request) {
	if !s.device(w, r) {
		return
	}
	if s.cfg.Self == "" {
		// Not 404: the phone takes that for a companion older than /api/cli and runs the command as a shell line.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "this companion does not know where its program is"})
		return
	}
	var body struct {
		Args []string `json:"args"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if !cliOK(body.Args) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a command the phone may run"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cliTimeout)
	defer cancel()
	args := body.Args
	if s.cfg.HC != "" && (args[0] == "track" || args[1] == "on" || args[1] == "off" || args[1] == "organizer") {
		args = append(append([]string{}, args...), "--hc", s.cfg.HC)
	}
	cmd := proc.CommandContext(ctx, s.cfg.Self, args...)
	cmd.Env = append(os.Environ(), "HERDR_ANYWHERE_STATE="+s.cfg.Store.Dir())
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	code := 0
	var x *exec.ExitError
	if errors.As(err, &x) {
		code = x.ExitCode()
	} else if err != nil {
		s.log.Printf("cli: %s %s: %v", body.Args[0], body.Args[1], err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not run herdr-anywhere"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "stdout": strings.ToValidUTF8(out.String(), "\uFFFD"), "stderr": strings.ToValidUTF8(errs.String(), "\uFFFD")})
}
