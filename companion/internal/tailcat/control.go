package tailcat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// `herdr-anywhere pair` and `serve` are separate processes, and only serve can
// open a Tailcat pairing window: the window changes the allowlist of the
// Host serve holds. So serve --tailcat listens on a unix socket in the state
// dir and pair POSTs its pairing code there:
//
//	POST /pair {"code":…} → 200 {"invite","id","expires","urls"} | 500 {"error"}
//	GET /pair/{id} → 200 Status | 404 {"error"}
//	POST /pair/{id}/answer {"approve":bool} → 200 {} | 404 | 409 {"error"}

// SocketName is the control socket in the state dir.
const SocketName = "serve.sock"

// ErrNoServe means no serve --tailcat is listening on the state dir.
var ErrNoServe = errors.New("no `herdr-anywhere serve --tailcat` is running on this state dir")

// PairReply is serve's answer to pair.
type PairReply struct {
	Window
	// URLs are serve's http listen addresses, for the LAN pairing link.
	URLs []string `json:"urls,omitempty"`
}

// macOS's sockaddr_un holds 104 bytes, the terminating NUL included.
const maxSocketPath = 103

func socketPath(dir string) (string, error) {
	p := filepath.Join(dir, SocketName)
	if len(p) > maxSocketPath {
		return "", fmt.Errorf("%s: longer than the %d bytes a unix socket path may have; use a shorter --state dir", p, maxSocketPath)
	}
	return p, nil
}

// Listen takes the control socket, replacing one a crashed serve left behind.
// The socket is 0600 inside the 0700 state dir: only this user can open
// pairing windows.
func Listen(dir string) (net.Listener, error) {
	p, err := socketPath(dir)
	if err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", p, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("another `herdr-anywhere serve --tailcat` is running on %s", dir)
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	if err := restrictSocket(p); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Windows follows the windows open opened, and answers the phone asking on one (Service).
type Windows interface {
	Status(id string) (Status, error)
	Answer(id string, approve bool) error
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func windowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoWindow):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, ErrNotAsking):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

type noWindows struct{}

func (noWindows) Status(string) (Status, error) { return Status{}, ErrNoWindow }
func (noWindows) Answer(string, bool) error     { return ErrNoWindow }

// ControlHandler answers pair's requests by calling open with the code, and ws (nil: none) about the windows it opened.
func ControlHandler(open func(ctx context.Context, code string) (Window, error), ws Windows, urls []string) http.Handler {
	mux := http.NewServeMux()
	if ws == nil {
		ws = noWindows{}
	}
	mux.HandleFunc("GET /pair/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		st, err := ws.Status(r.PathValue("id"))
		if err != nil {
			windowError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("POST /pair/{id}/answer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Approve *bool `json:"approve"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Approve == nil {
			writeError(w, http.StatusBadRequest, errors.New(`body must be {"approve":true|false}`))
			return
		}
		if err := ws.Answer(r.PathValue("id"), *req.Approve); err != nil {
			windowError(w, err)
			return
		}
		_, _ = io.WriteString(w, "{}\n")
	})
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code string `json:"code"`
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "body must be {\"code\":…}"})
			return
		}
		win, err := open(r.Context(), req.Code)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(PairReply{Window: win, URLs: urls})
	})
	return mux
}

// ServeRunning reports whether a serve --tailcat answers on dir's control socket.
// A socket file a crashed serve left behind does not count.
func ServeRunning(dir string) bool {
	p, err := socketPath(dir)
	if err != nil {
		return false
	}
	c, err := net.DialTimeout("unix", p, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// call sends one request to the serve on dir and decodes its answer into out; an error status is serve's error.
func call(ctx context.Context, dir, method, path string, body, out any) error {
	p, err := socketPath(dir)
	if err != nil {
		return ErrNoServe // serve could not have listened there either
	}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", p)
		},
	}}
	defer client.CloseIdleConnections()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://serve"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if noServe(err) {
		return ErrNoServe
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		switch {
		case resp.StatusCode == http.StatusNotFound && e.Error == ErrNoWindow.Error():
			return ErrNoWindow
		case resp.StatusCode == http.StatusConflict:
			return ErrNotAsking
		case e.Error == "":
			// A serve from before the emoji check has no such route.
			return fmt.Errorf("serve answered %s", resp.Status)
		}
		return errors.New(e.Error)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}

// RequestPair asks the serve on dir to open a pairing window for code.
func RequestPair(ctx context.Context, dir, code string) (PairReply, error) {
	var r PairReply
	err := call(ctx, dir, http.MethodPost, "/pair", map[string]string{"code": code}, &r)
	if err != nil && !errors.Is(err, ErrNoServe) {
		return PairReply{}, fmt.Errorf("serve could not open a Tailcat pairing window: %w", err)
	}
	return r, err
}

// PairStatus asks the serve on dir about window id.
func PairStatus(ctx context.Context, dir, id string) (Status, error) {
	var st Status
	err := call(ctx, dir, http.MethodGet, "/pair/"+url.PathEscape(id), nil, &st)
	return st, err
}

// AnswerPair answers the phone asking on window id.
func AnswerPair(ctx context.Context, dir, id string, approve bool) error {
	var out struct{}
	return call(ctx, dir, http.MethodPost, "/pair/"+url.PathEscape(id)+"/answer", map[string]bool{"approve": approve}, &out)
}
