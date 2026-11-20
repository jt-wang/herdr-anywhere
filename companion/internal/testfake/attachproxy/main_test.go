package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Wire envelopes mirror app/src/hc/client.ts and client.test.ts; frame layout
// mirrors frames.ts. No external agent database or file schema is synthesized.
func wire(ch uint32, kind byte, payload string) []byte {
	b := make([]byte, 9+len(payload))
	binary.LittleEndian.PutUint32(b, ch)
	b[4] = kind
	binary.LittleEndian.PutUint32(b[5:], uint32(len(payload)))
	copy(b[9:], payload)
	return b
}

func TestAttachFaults(t *testing.T) {
	for _, b64 := range []bool{false, true} {
		t.Run(fmt.Sprintf("base64=%v", b64), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			peers := make(chan *websocket.Conn, 4)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/hc" {
					w.Header().Set("X-Proxy-Test", "yes")
					_, _ = io.WriteString(w, r.Method+" "+r.URL.RequestURI())
					return
				}
				if (r.URL.Query().Get("enc") == "b64") != b64 || r.URL.Query().Get("token") != "synthetic-token" || r.Header.Get("Origin") != "" {
					t.Error("upstream query/Origin mismatch")
				}
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error("upstream accept failed")
					return
				}
				peers <- c
			}))
			defer up.Close()
			u, _ := url.Parse(up.URL)
			p := newProxy(u)
			srv := httptest.NewServer(p)
			defer srv.Close()
			control := func(mode string) {
				t.Helper()
				r, err := http.Get(srv.URL + "/fault?mode=" + mode)
				if err != nil {
					t.Fatal(err)
				}
				r.Body.Close()
				if r.StatusCode != http.StatusNoContent {
					t.Fatalf("fault status %d", r.StatusCode)
				}
			}
			stats := func() map[string]uint64 {
				t.Helper()
				r, err := http.Get(srv.URL + "/stats")
				if err != nil {
					t.Fatal(err)
				}
				defer r.Body.Close()
				var s map[string]uint64
				if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
					t.Fatal(err)
				}
				return s
			}
			write := func(c *websocket.Conn, b []byte) {
				t.Helper()
				typ := websocket.MessageBinary
				if b64 {
					typ, b = websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(b))
				}
				if err := c.Write(ctx, typ, b); err != nil {
					t.Fatal(err)
				}
			}
			read := func(c *websocket.Conn, want []byte) {
				t.Helper()
				typ, got, err := c.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if b64 {
					if typ != websocket.MessageText {
						t.Fatal("expected base64 text")
					}
					got, err = base64.StdEncoding.DecodeString(string(got))
				} else if typ != websocket.MessageBinary {
					t.Fatal("expected binary")
				}
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("wire mismatch: got %q want %q (%v)", got, want, err)
				}
			}
			dial := func() (*websocket.Conn, *websocket.Conn) {
				t.Helper()
				q := "?token=synthetic-token"
				if b64 {
					q += "&enc=b64"
				}
				c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/hc"+q, nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = c.CloseNow() })
				select {
				case peer := <-peers:
					t.Cleanup(func() { _ = peer.CloseNow() })
					return c, peer
				case <-ctx.Done():
					t.Fatal("no upstream connection")
					return nil, nil
				}
			}

			// Ordinary HTTP still passes method, path, query, body and headers.
			r, err := http.Post(srv.URL+"/pair?fixture=yes", "text/plain", strings.NewReader("fixture"))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if string(body) != "POST /pair?fixture=yes" || r.Header.Get("X-Proxy-Test") != "yes" {
				t.Fatal("HTTP reverse proxy mismatch")
			}
			control("hold")
			client, core := dial()
			hello := []byte("{\"jsonrpc\":\"2.0\",\"id\":0,\"method\":\"hello\",\"params\":{\"framed\":true}}\n")
			write(client, hello[:13])
			write(client, hello[13:])
			read(core, hello)
			helloReply := []byte("{\"jsonrpc\":\"2.0\",\"id\":0,\"result\":{\"generation\":1,\"version\":\"0.1.13\",\"framed\":true}}\n")
			heartbeat := wire(3, 0, "{\"jsonrpc\":\"2.0\",\"method\":\"heartbeat\",\"params\":{\"seq\":5}}\n")
			write(core, helloReply[:17])
			write(core, append(bytes.Clone(helloReply[17:]), heartbeat...))
			read(client, helloReply)
			read(client, heartbeat)
			a := wire(0, 0, `{"jsonrpc":"2.0","id":1,"method":"terminal.attach","params":{"terminal":"t1","cols":80,"rows":24}}`)
			b := wire(0, 0, `{"jsonrpc":"2.0","id":2,"method":"terminal.attach","params":{"terminal":"t2","cols":80,"rows":24}}`)
			other := wire(0, 0, `{"jsonrpc":"2.0","id":3,"method":"terminal.list","params":{}}`)
			write(client, a[:6])
			write(client, bytes.Join([][]byte{a[6:], b, other}, nil))
			read(core, a)
			read(core, b)
			read(core, other)
			reply := wire(0, 0, `{"jsonrpc":"2.0","id":1,"result":{"channel":9,"terminal":{"address":"t1"}}}`)
			failure := wire(0, 0, `{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"no terminal","data":{"kind":"not_found","hint":"h"}}}`)
			paint := wire(9, 0, "early paint")
			listed := wire(0, 0, `{"jsonrpc":"2.0","id":3,"result":{"terminals":[]}}`)
			event := wire(3, 0, "{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"seq\":5,\"type\":\"title\",\"terminal\":\"t1\",\"data\":{\"title\":\"fixture\"}}}\n")
			write(core, bytes.Join([][]byte{reply, paint, listed, failure, event, heartbeat}, nil))
			// A held response appearing here fails these reads, without a timing assertion.
			read(client, paint)
			read(client, listed)
			read(client, event)
			read(client, heartbeat)
			if s := stats(); s["attach_requests"] != 2 || s["held_responses"] != 2 || s["released_responses"] != 0 || s["active_connections"] != 1 {
				t.Fatalf("hold stats: %v", s)
			}
			// Client traffic remains live while replies are held, too.
			for _, f := range [][]byte{wire(9, 0, "keys"), wire(9, 1, "\x00\x01\x00\x00"), wire(9, 3, "\x50\x00\x18\x00"), wire(9, 4, "\x01")} {
				write(client, f)
				read(core, f)
			}
			control("pass")
			read(client, reply)
			read(client, failure)
			// A subsequent attach must pass normally, after the released replies.
			write(client, a)
			read(core, a)
			write(core, reply)
			read(client, reply)
			closeFrame := wire(9, 2, "")
			write(client, closeFrame)
			read(core, closeFrame)
			if s := stats(); s["attach_requests"] != 3 || s["released_responses"] != 2 || s["client_held_closes"] != 1 {
				t.Fatalf("release stats: %v", s)
			}
			// Re-arm an existing connection, then discard its queued reply.
			control("hold")
			write(client, a)
			read(core, a)
			write(core, append(bytes.Clone(reply), heartbeat...))
			read(client, heartbeat)
			if s := stats(); s["held_responses"] != 3 || s["released_responses"] != 2 {
				t.Fatalf("re-arm stats: %v", s)
			}
			control("drop")
			if _, _, err := client.Read(ctx); err == nil {
				t.Fatal("drop left client open")
			}
			if _, _, err := core.Read(ctx); err == nil {
				t.Fatal("drop left upstream open")
			}
			// Drop is one-shot, not a persistent refusal of future sockets.
			next, nextCore := dial()
			write(next, hello)
			read(nextCore, hello)
			write(nextCore, helloReply)
			read(next, helloReply)
			control("pass")
			write(nextCore, heartbeat)
			read(next, heartbeat) // no queued frame from the dropped socket
			control("drop")
			deadline := time.Now().Add(time.Second)
			for stats()["active_connections"] != 0 {
				if time.Now().After(deadline) {
					t.Fatal("connection handlers did not finish")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
