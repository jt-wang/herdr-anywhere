// Command serve runs the fake Gemini API on an address, for the e2e scripts: go run ./internal/chat/fakegemini/serve 127.0.0.1:PORT
// FAKEGEMINI_DUMP=<folder> writes each request's body there, to see what agy sends.
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

	"herdr-anywhere/companion/internal/chat/fakegemini"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: serve HOST:PORT")
	}
	s := &fakegemini.Server{}
	dump := os.Getenv("FAKEGEMINI_DUMP")
	var n atomic.Int64
	log.Printf("fake Gemini API on %s", os.Args[1])
	log.Fatal(http.ListenAndServe(os.Args[1], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		if dump != "" {
			b, _ := io.ReadAll(r.Body)
			os.WriteFile(filepath.Join(dump, fmt.Sprintf("%04d.json", n.Add(1))), b, 0o644)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		s.ServeHTTP(w, r)
	})))
}
