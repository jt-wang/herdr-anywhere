// Command serve runs the fake Anthropic API on an address, for the e2e scripts: go run ./internal/chat/fakeapi/serve 127.0.0.1:PORT
package main

import (
	"log"
	"net/http"
	"os"

	"herdr-anywhere/companion/internal/chat/fakeapi"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: serve HOST:PORT")
	}
	s := &fakeapi.Server{}
	log.Printf("fake Anthropic API on %s", os.Args[1])
	log.Fatal(http.ListenAndServe(os.Args[1], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		s.ServeHTTP(w, r)
	})))
}
