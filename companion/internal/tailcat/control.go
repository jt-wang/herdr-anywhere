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
	"os"
	"path/filepath"
	"time"
)

// `herdr-anywhere pair` and `serve` are separate processes, and only serve can
// open a Tailcat pairing window: the window changes the allowlist of the
// Host serve holds. So serve --tailcat listens on a unix socket in the state
// dir and pair POSTs its pairing code there:
//
//	POST /pair {"code":…} → 200 {"invite","expires","urls"} | 500 {"error"}

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

// ControlHandler answers pair's requests by calling open with the code.
func ControlHandler(open func(ctx context.Context, code string) (Window, error), urls []string) http.Handler {
	mux := http.NewServeMux()
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

// RequestPair asks the serve on dir to open a pairing window for code.
func RequestPair(ctx context.Context, dir, code string) (PairReply, error) {
	p, err := socketPath(dir)
	if err != nil {
		return PairReply{}, ErrNoServe // serve could not have listened there either
	}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", p)
		},
	}}
	defer client.CloseIdleConnections()
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://serve/pair", bytes.NewReader(body))
	if err != nil {
		return PairReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if noServe(err) {
		return PairReply{}, ErrNoServe
	}
	if err != nil {
		return PairReply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		return PairReply{}, fmt.Errorf("serve could not open a Tailcat pairing window: %s", e.Error)
	}
	var r PairReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r); err != nil {
		return PairReply{}, err
	}
	return r, nil
}
