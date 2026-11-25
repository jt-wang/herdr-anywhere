package chat

import (
	"strings"
	"testing"
)

type titleReader struct {
	fakeReader
	calls int
	text  string
}

func (r *titleReader) Title(string) (string, error) { r.calls++; return r.text, nil }
func (r *titleReader) Turns(string) ([]Turn, error) { panic("read chat") }
func TestTitleOnly(t *testing.T) {
	r := &titleReader{fakeReader: fakeReader{path: "/fictional"}, text: " Repair\n deck\t rails\x00\u202e "}
	s, _, _ := service(working, r)
	a := s.Title("t1")
	if a.Title != "Repair deck rails" || a.Session == "" {
		t.Fatalf("%+v", a)
	}
	if r.calls != 1 || len(r.finds) != 1 || r.finds[0] != 4242 {
		t.Fatal("HC discovery")
	}
	if a.Session != s.Title("t1").Session {
		t.Fatal("unstable")
	}
	r.path = "/other"
	if a.Session == s.Title("t1").Session {
		t.Fatal("wrong session")
	}
	r.text = strings.Repeat("界", 300)
	if len([]rune(s.Title("t1").Title)) > 160 {
		t.Fatal("long")
	}
}
func TestTitleRejectsDiscovery(t *testing.T) {
	for _, reason := range []string{Ambiguous, NoMatch} {
		r := &titleReader{fakeReader: fakeReader{path: "/wrong", findErr: &Miss{Reason: reason}}, text: "Wrong"}
		s, _, _ := service(working, r)
		if a := s.Title("t1"); a.Title != "" || a.Session != "" || r.calls != 0 {
			t.Fatal(a)
		}
	}
}
func TestTitleRejectsAddress(t *testing.T) {
	r := &titleReader{text: "Wrong"}
	s, _, asked := service(working, r)
	for _, a := range []string{"other/t1", "../t1", "", "-arg"} {
		if got := s.Title(a); got.Title != "" {
			t.Fatal(got)
		}
	}
	if len(*asked) > 0 || r.calls > 0 {
		t.Fatal("invalid lookup")
	}
}
