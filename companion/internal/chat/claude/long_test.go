package claude

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// A turn that runs long: commands whose outputs are about 1 MB each, n of them, after the prompt; the last one still
// running. A made-up record: the outputs quote a prompt line, as a tool reading a record would print it.
func longTurn(n int) []string {
	out := strings.Repeat(`{"type":"user","turnPosition":{"promptIndex":0}} `, 1<<20/50)
	ls := []string{prompt("u0", "an earlier question"), say("a0", "m0", "an earlier answer", "end_turn"), prompt("u1", "build the docs site")}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("toolu_%d", i)
		ls = append(ls, use(fmt.Sprintf("a%d", i+1), fmt.Sprintf("m%d", i+1), id, "Bash", fmt.Sprintf(`{"command":"make page-%d"}`, i)), result(fmt.Sprintf("r%d", i), id, out))
		if i == 1 { // a subagent's prompt, written to this record as a side chain: not where this turn began
			ls = append(ls, strings.Replace(strings.Replace(prompt("s1", "check the links"), `"type":"user",`, `"type":"user","isSidechain":true,`, 1), `"turnOrigin":"human"`, `"turnOrigin":"agent"`, 1))
		}
	}
	return append(ls, use("a-last", "m-last", "toolu_last", "Bash", `{"command":"make index"}`))
}

// The prompt is further back than MaxTail: the turn is read from it, open, with its steps.
func TestTurnLongerThanTheTail(t *testing.T) {
	p := write(t, longTurn(6)...)
	if st, _ := os.Stat(p); st.Size() < chat.MaxTail+(2<<20) {
		t.Fatalf("the record is %d bytes, want the prompt well before MaxTail", st.Size())
	}
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("no turns")
	}
	last := ts[len(ts)-1]
	if last.Said != "build the docs site" || last.End != "open" || len(last.Steps) != 7 {
		t.Fatalf("last turn %q, %s, %d steps", last.Said, last.End, len(last.Steps))
	}
	// The first step's details are found, though it is more than MaxTail from the end.
	d, err := r.Detail(p, "toolu_0")
	if err != nil || !strings.Contains(d.Input, "make page-0") {
		t.Fatalf("detail of the first step: %+v %v", d, err)
	}
}

// One tool output over 4 MB (MaxTail): read whole, no format error.
func TestALineOverMaxTail(t *testing.T) {
	big := strings.Repeat("y", 5<<20)
	ts := turns(t, prompt("u1", "print the log"), use("a1", "m1", "toolu_1", "Bash", `{"command":"cat big.log"}`), result("r1", "toolu_1", big), say("a2", "m2", "It is long.", "end_turn"))
	if len(ts) != 1 || ts[0].Said != "print the log" || ts[0].End != "done" || ts[0].Reply != "It is long." {
		t.Fatalf("%+v", ts)
	}
	// And still running when the output is written.
	ts = turns(t, prompt("u1", "print the log"), use("a1", "m1", "toolu_1", "Bash", `{"command":"cat big.log"}`), result("r1", "toolu_1", big))
	if len(ts) != 1 || ts[0].End != "open" {
		t.Fatalf("%+v", ts)
	}
}

// The prompt is further back than MaxBack: the turn shows from where the reading starts, open, without its prompt;
// never no turn at all (the phone would show an empty page).
func TestPromptBeyondMaxBack(t *testing.T) {
	p := write(t, longTurn(chat.MaxBack>>20+2)...)
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].Said != "" || ts[0].End != "open" || len(ts[0].Steps) == 0 {
		t.Fatalf("%d turns: %+v", len(ts), ts)
	}
	if s := ts[0].Steps[len(ts[0].Steps)-1]; s.ID != "toolu_last" {
		t.Fatalf("last step %+v", s)
	}
	// Done once the agent answers.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(say("a-end", "m-end", "The site is built.", "end_turn") + "\n")
	f.Close()
	ts, err = (&Reader{}).Turns(p)
	if err != nil || len(ts) != 1 || ts[0].End != "done" || ts[0].Reply != "The site is built." {
		t.Fatalf("%+v %v", ts, err)
	}
	// A new prompt after it: that turn, from its prompt; the part without one is left out, as any turn the window
	// starts in.
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(prompt("u9", "now the index") + "\n")
	f.Close()
	ts, err = (&Reader{}).Turns(p)
	if err != nil || len(ts) != 1 || ts[0].Said != "now the index" || ts[0].End != "open" {
		t.Fatalf("%+v %v", ts, err)
	}
}

// A record with nothing but a prompt being written: no turn, no error.
func TestOnlyAHalfLine(t *testing.T) {
	p := write(t)
	os.WriteFile(p, []byte(`{"type":"user","uuid":"u1","turnPosition":`), 0o644)
	ts, err := (&Reader{}).Turns(p)
	var fe *chat.FormatError
	if err != nil || errors.As(err, &fe) || len(ts) != 0 {
		t.Fatalf("%+v %v", ts, err)
	}
}
