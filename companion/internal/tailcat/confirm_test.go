package tailcat

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"
)

var testEmoji = []string{"🐶", "🌵", "🔑", "🚲"}

// asking waits until window id has a phone asking, and returns what it asks.
func asking(t *testing.T, s *Service, id string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if st.State == StateAsking {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no phone asked")
	return Status{}
}

func confirmAsync(w *window, ctx context.Context) <-chan bool {
	got := make(chan bool, 1)
	go func() {
		got <- w.confirm(ctx, connecthost.PairRequest{Phone: "nodekey:x", Device: "Pixel", Route: connecthost.RouteLAN, Emoji: testEmoji})
	}()
	return got
}

// The phone waits for the computer's user, who sees its name and emoji, and only a yes lets it pair.
func TestConfirmWaitsForTheUsersAnswer(t *testing.T) {
	for _, approve := range []bool{true, false} {
		s := &Service{}
		id, w := s.windows.add()
		if st, _ := s.Status(id); st.State != StateWaiting {
			t.Fatalf("a new window: %+v", st)
		}
		if err := s.Answer(id, true); !errors.Is(err, ErrNotAsking) {
			t.Fatalf("an answer with no phone asking: %v", err)
		}
		got := confirmAsync(w, context.Background())
		st := asking(t, s, id)
		if st.Request == nil || st.Request.Device != "Pixel" || !slices.Equal(st.Request.Emoji, testEmoji) || st.Request.Route != connecthost.RouteLAN {
			t.Fatalf("the user is shown %+v", st.Request)
		}
		select {
		case <-got:
			t.Fatal("confirm returned before the user answered")
		case <-time.After(50 * time.Millisecond):
		}
		if err := s.Answer(id, approve); err != nil {
			t.Fatal(err)
		}
		if ok := <-got; ok != approve {
			t.Fatalf("answer %v, confirm said %v", approve, ok)
		}
		if err := s.Answer(id, true); !errors.Is(err, ErrNotAsking) {
			t.Fatalf("a second answer: %v", err)
		}
	}
}

// While the computer's user is asked about one phone, a second one asking the same window is a no at once, and the
// user's yes still goes to the first.
func TestConfirmAsksAboutOnePhoneAtATime(t *testing.T) {
	s := &Service{}
	id, w := s.windows.add()
	first := confirmAsync(w, context.Background())
	asking(t, s, id)
	select {
	case ok := <-confirmAsync(w, context.Background()):
		if ok {
			t.Fatal("a second phone was confirmed while the user was asked about the first")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second phone waits on a window that is already asking")
	}
	if err := s.Answer(id, true); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-first:
		if !ok {
			t.Fatal("the first phone did not get the user's yes")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first phone never heard the user's yes")
	}
}

// A phone that goes away (or the 55 s that run out) is no yes, and leaves nothing to answer.
func TestConfirmIsNoWhenThePhoneLeaves(t *testing.T) {
	s := &Service{}
	id, w := s.windows.add()
	ctx, cancel := context.WithCancel(context.Background())
	got := confirmAsync(w, ctx)
	asking(t, s, id)
	cancel()
	if <-got {
		t.Fatal("confirmed a phone that left")
	}
	if st, _ := s.Status(id); st.State != StateWaiting {
		t.Fatalf("after the phone left: %+v", st)
	}
	if err := s.Answer(id, true); !errors.Is(err, ErrNotAsking) {
		t.Fatalf("an answer after the phone left: %v", err)
	}
}

// A closed window says how it ended, and asks no more.
func TestAWindowThatEndedSaysHow(t *testing.T) {
	s := &Service{}
	id, w := s.windows.add()
	w.end("Pixel")
	if st, _ := s.Status(id); st.State != StatePaired || st.Device != "Pixel" {
		t.Fatalf("paired: %+v", st)
	}
	id2, w2 := s.windows.add()
	w2.end("")
	if st, _ := s.Status(id2); st.State != StateEnded {
		t.Fatalf("ended: %+v", st)
	}
	if <-confirmAsync(w2, context.Background()) {
		t.Fatal("a closed window confirmed a phone")
	}
	s.windows.forget(id)
	if _, err := s.Status(id); !errors.Is(err, ErrNoWindow) {
		t.Fatalf("a forgotten window: %v", err)
	}
}

// pair follows its window and answers over the control socket.
func TestControlSocketFollowsAndAnswersAWindow(t *testing.T) {
	st := newStore(t)
	s := &Service{}
	id, w := s.windows.add()
	open := func(context.Context, string) (Window, error) {
		return Window{Invite: Prefix2 + "abc", ID: id, Expires: time.Now().Add(time.Minute)}, nil
	}
	serveControl(t, st.Dir(), ControlHandler(open, s, nil))
	ctx := context.Background()
	r, err := RequestPair(ctx, st.Dir(), "code")
	if err != nil || r.ID != id {
		t.Fatalf("reply %+v, %v", r, err)
	}
	if _, err := PairStatus(ctx, st.Dir(), "nope"); !errors.Is(err, ErrNoWindow) {
		t.Fatalf("unknown window: %v", err)
	}
	if err := AnswerPair(ctx, st.Dir(), id, true); !errors.Is(err, ErrNotAsking) {
		t.Fatalf("answer with no phone asking: %v", err)
	}
	got := confirmAsync(w, ctx)
	for {
		ws, err := PairStatus(ctx, st.Dir(), id)
		if err != nil {
			t.Fatal(err)
		}
		if ws.State == StateAsking {
			if ws.Request == nil || !slices.Equal(ws.Request.Emoji, testEmoji) {
				t.Fatalf("status %+v", ws)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := AnswerPair(ctx, st.Dir(), id, true); err != nil {
		t.Fatal(err)
	}
	if !<-got {
		t.Fatal("yes over the socket did not confirm")
	}
}
