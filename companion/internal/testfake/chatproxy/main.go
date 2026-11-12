// Command chatproxy sits between the app and the Companion in an e2e (app/e2e/ios-chatfold.sh) and passes everything
// on. Through files in a folder it changes the phone's chat answers (GET /api/chat/{terminal}) as a slow or unsure
// computer would:
//
//	no-status  the next chat answer only says the computer can't tell (as when herdr-core reports no status);
//	           the file becomes no-status-sent
//	hold       the first chat answer whose last turn is still open is kept (the file becomes held) until a file
//	           release appears, then sent (released): an older answer landing after newer ones
//
// Usage: chatproxy -listen 127.0.0.1:PORT -to http://127.0.0.1:PORT -ctl FOLDER
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "where the app connects")
	to := flag.String("to", "", "the Companion's URL")
	ctl := flag.String("ctl", "", "the folder of control files")
	flag.Parse()
	u, err := url.Parse(*to)
	if err != nil || u.Host == "" || *ctl == "" {
		fmt.Fprintln(os.Stderr, "usage: chatproxy -listen ADDR -to URL -ctl FOLDER")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, http.ListenAndServe(*listen, New(u, *ctl)))
	os.Exit(1)
}

// New passes every request on to the Companion at to, changing chat answers as the files in ctl say.
func New(to *url.URL, ctl string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(to)
	file := func(name string) string { return filepath.Join(ctl, name) }
	exists := func(name string) bool { _, err := os.Stat(file(name)); return err == nil }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, isChat := strings.CutPrefix(r.URL.EscapedPath(), "/api/chat/")
		if r.Method != http.MethodGet || !isChat || addr == "" || strings.Contains(addr, "/") {
			proxy.ServeHTTP(w, r)
			return
		}
		// A rename is taken once, however many asks overlap.
		if os.Rename(file("no-status"), file("no-status-sent")) == nil {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"chat":true,"unknown":"no-status"}`)
			return
		}
		if !exists("hold") {
			proxy.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		r.Header.Del("Accept-Encoding")
		proxy.ServeHTTP(rec, r)
		var a struct {
			Turns []struct {
				End string `json:"end"`
			} `json:"turns"`
		}
		open := json.Unmarshal(rec.Body.Bytes(), &a) == nil && len(a.Turns) > 0 && a.Turns[len(a.Turns)-1].End == "open"
		if open && os.Rename(file("hold"), file("held")) == nil {
			for end := time.Now().Add(2 * time.Minute); !exists("release") && time.Now().Before(end); {
				time.Sleep(50 * time.Millisecond)
			}
			_ = os.Remove(file("release"))
			defer func() { _ = os.WriteFile(file("released"), nil, 0o644) }()
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
}
