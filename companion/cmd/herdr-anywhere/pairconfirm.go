package main

// The emoji check (TailPocket v0.3.0-pre.4 and later): a phone pairing through a window waits until this computer's
// user has compared the four emoji it shows with the ones here and said yes. pair asks y/N in its terminal; the Add
// phone dialog asks with its buttons, through pair --json --window ID [--answer yes|no].

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// pairInput is where pair reads the user's yes or no; a var for tests.
var pairInput io.Reader = os.Stdin

// pairPoll is how often pair asks serve what its window is doing.
var pairPoll = 300 * time.Millisecond

// errNotPaired: the window closed without a phone; what happened is already on stdout.
var errNotPaired = errors.New("no phone paired; run herdr-anywhere pair for a new code")

// followPair waits for a phone on window r.ID, asks the user about it, and says how it ended.
func followPair(dir string, r tailcat.PairReply, in io.Reader, w io.Writer) error {
	ctx, cancel := context.WithDeadline(context.Background(), r.Expires.Add(30*time.Second))
	defer cancel()
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	fmt.Fprintln(w, "Waiting for the phone. When it shows four emoji, this computer asks you to compare them.")
	for {
		st, err := tailcat.PairStatus(ctx, dir, r.ID)
		if err != nil {
			if ctx.Err() != nil {
				err = errors.New("the code expired")
			}
			return fmt.Errorf("pairing: %w", err)
		}
		switch st.State {
		case tailcat.StatePaired:
			fmt.Fprintf(w, "%s is paired.\n", st.Device)
			return nil
		case tailcat.StateEnded:
			fmt.Fprintln(w, "This code no longer works: it expired, or a pairing was refused or given up.")
			return errNotPaired
		case tailcat.StateAsking:
			if err := ask(ctx, dir, r.ID, st.Request, lines, w); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(pairPoll):
		}
	}
}

// ask shows the phone asking and sends the user's answer; a phone that stops asking first is no answer.
func ask(ctx context.Context, dir, id string, req *tailcat.Request, lines <-chan string, w io.Writer) error {
	if req == nil {
		return nil
	}
	fmt.Fprintf(w, "\n%s wants to pair with this computer. Check that the phone shows the same four emoji:\n\n    %s\n\n",
		req.Device, strings.Join(req.Emoji, "  "))
	fmt.Fprintf(w, "Pair %s? Only if the emoji are the same [y/N]: ", req.Device)
	tick := time.NewTicker(pairPoll)
	defer tick.Stop()
	for {
		select {
		case line, ok := <-lines:
			approve := ok && yes(line)
			if !ok {
				fmt.Fprintln(w)
			}
			if err := tailcat.AnswerPair(ctx, dir, id, approve); err != nil && !errors.Is(err, tailcat.ErrNotAsking) {
				return fmt.Errorf("pairing: %w", err)
			}
			if !approve {
				fmt.Fprintln(w, "Not paired. This code no longer works.")
				return errNotPaired
			}
			return nil
		case <-tick.C:
			st, err := tailcat.PairStatus(ctx, dir, id)
			if err == nil && st.State == tailcat.StateAsking {
				continue
			}
			fmt.Fprintln(w, "\nThe phone stopped waiting, or no answer came within 55 seconds.")
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func yes(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// windowJSON is pair --json --window: the window's state, after answering the phone asking when answer is yes or no.
func windowJSON(st *store.Store, id, answer string, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), execLimit)
	defer cancel()
	if answer != "" {
		if err := tailcat.AnswerPair(ctx, st.Dir(), id, answer == "yes"); err != nil {
			return windowError(stdout, err)
		}
	}
	s, err := tailcat.PairStatus(ctx, st.Dir(), id)
	if err != nil {
		return windowError(stdout, err)
	}
	return json.NewEncoder(stdout).Encode(s)
}

func windowError(stdout io.Writer, err error) error {
	switch {
	case errors.Is(err, tailcat.ErrNoServe):
		return reportJSON(stdout, "not-running")
	case errors.Is(err, tailcat.ErrNoWindow):
		return reportJSON(stdout, "no-window")
	case errors.Is(err, tailcat.ErrNotAsking):
		return reportJSON(stdout, "not-asking")
	}
	return reportJSON(stdout, err.Error())
}
