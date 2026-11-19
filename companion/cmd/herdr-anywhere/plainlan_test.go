package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/store"
)

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := ln.Addr().String()
	ln.Close()
	return a
}

// The plain LAN port opens only with --lan, a phone paired over it before 0.3, and time left; a phone paired through
// Tailcat does not count.
func TestPlainLANOpensOnlyForPhonesThatNeedIt(t *testing.T) {
	now := time.Now()
	lanAddrs := []netip.Addr{netip.MustParseAddr("192.168.1.5")}
	listen := []string{"127.0.0.1:7681"}

	st, _ := store.Open(shortStateDir(t))
	code, _ := st.CreateCode("p", time.Minute)
	if _, _, err := st.RedeemTailcat(code, "Tailcat phone", "nodekey:00"); err != nil {
		t.Fatal(err)
	}
	p, err := openPlainLAN(st, true, listen, lanAddrs, listen, now)
	if err != nil || !p.Until().IsZero() {
		t.Fatalf("only a Tailcat phone: until %v (%v), want closed", p.Until(), err)
	}

	st2, _ := store.Open(shortStateDir(t))
	code, _ = st2.CreateCode("p", time.Minute)
	if _, _, err := st2.Redeem(code, "LAN phone"); err != nil {
		t.Fatal(err)
	}
	if p, _ := openPlainLAN(st2, false, listen, lanAddrs, listen, now); !p.Until().IsZero() {
		t.Fatal("opened without --lan")
	}
	p, err = openPlainLAN(st2, true, listen, lanAddrs, listen, now)
	// The deadline counts from the first serve of 0.3 (the call above), saved to the second.
	if d := now.Add(72 * time.Hour).Sub(p.Until()); err != nil || d < 0 || d >= time.Second {
		t.Fatalf("LAN phone: until %v (%v)", p.Until(), err)
	}
	if strings.Join(p.addrs, ",") != "192.168.1.5:7681" {
		t.Fatalf("plain addresses %v: want the LAN address on serve's port, not loopback again", p.addrs)
	}
	if p, _ := openPlainLAN(st2, true, listen, lanAddrs, listen, now.Add(73*time.Hour)); !p.Until().IsZero() {
		t.Fatal("opened after its 3 days")
	}
}

// On the plain port nothing pairs, and a phone that has a Tailcat key (its end-to-end routes) is
// refused with its own token, by header or query: it must never fall back to plain HTTP. A phone
// paired over plain HTTP before 0.3 is still served.
func TestPlainPortServesOnlyPlainPhones(t *testing.T) {
	st, _ := store.Open(shortStateDir(t))
	code, _ := st.CreateCode("p", time.Minute)
	_, plainToken, err := st.Redeem(code, "LAN phone")
	if err != nil {
		t.Fatal(err)
	}
	code, _ = st.CreateCode("p", time.Minute)
	_, tcToken, err := st.RedeemTailcat(code, "Tailcat phone", "nodekey:00")
	if err != nil {
		t.Fatal(err)
	}
	h := plainOnly(st, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	cases := []struct {
		name, path, header string
		want               int
	}{
		{"pair", "/api/pair", "", 403},
		{"pair subpath", "/api/pair/x", "", 403},
		{"no token", "/api/info", "", 200},
		{"plain phone, header", "/api/folders", "Bearer " + plainToken, 200},
		{"plain phone, query", "/hc?token=" + plainToken, "", 200},
		{"Tailcat phone, header", "/api/folders", "Bearer " + tcToken, 403},
		{"Tailcat phone, query", "/hc?token=" + tcToken, "", 403},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", c.path, nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

// Once the last phone paired over plain HTTP is gone (it paired again, or was removed), the port closes and stays
// closed: a later serve does not open it.
func TestPlainLANClosesWhenNoPhoneNeedsIt(t *testing.T) {
	restore := plainCheck
	plainCheck = 20 * time.Millisecond
	t.Cleanup(func() { plainCheck = restore })
	dir := shortStateDir(t)
	st, _ := store.Open(dir)
	code, _ := st.CreateCode("p", time.Minute)
	d, _, err := st.Redeem(code, "LAN phone")
	if err != nil {
		t.Fatal(err)
	}
	addr := freeLoopbackAddr(t)
	p := &plainLAN{st: st, addrs: []string{addr}, until: time.Now().Add(time.Hour)}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	if err := p.serve(ctx, ok, io.Discard, log.New(&logs, "", 0)); err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if r, err := http.Get("http://" + addr + "/api/info"); err != nil || r.StatusCode != 200 {
		t.Fatalf("plain port before: %v %v", r, err)
	}
	if err := st.Revoke(d.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !p.Until().IsZero() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.Until().IsZero() {
		t.Fatal("still open with no phone that needs it")
	}
	if _, err := http.Get("http://" + addr + "/api/info"); err == nil {
		t.Fatal("plain port still answers")
	}
	code, _ = st.CreateCode("p", time.Minute)
	st.Redeem(code, "another")
	if p, _ := openPlainLAN(st, true, nil, []netip.Addr{netip.MustParseAddr("192.168.1.5")}, []string{"127.0.0.1:7681"}, time.Now()); !p.Until().IsZero() {
		t.Fatal("a later serve opened it again")
	}
}
