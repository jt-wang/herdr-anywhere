// Command serve runs the fake Responses API on an address, for the e2e scripts: go run ./internal/chat/fakeresponses/serve 127.0.0.1:PORT
// With FAKE_DUMP=DIR it also saves each request's body there as 1.json, 2.json…, to see what a program sends.
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"herdr-anywhere/companion/internal/chat/fakeresponses"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: serve HOST:PORT")
	}
	s := &fakeresponses.Server{}
	dump := os.Getenv("FAKE_DUMP")
	var n atomic.Int64
	log.Printf("fake Responses API on %s", os.Args[1])
	log.Fatal(http.ListenAndServe(os.Args[1], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		if dump != "" && r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			os.WriteFile(filepath.Join(dump, fmt.Sprintf("%d.json", n.Add(1))), b, 0o644)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		s.ServeHTTP(w, r)
	})))
}
