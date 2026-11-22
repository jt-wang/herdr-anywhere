package main

// rotate-identity: this computer's TailPocket host gets new keys (Tailcat, Noise, relay), and every phone pairs again.
// The running serve does it, as the same process with the same arguments under whatever started it: it unpairs every
// phone while its old identity still reaches them (a connected phone hears 4401 and shows "Pair it again"), stops,
// moves tailcat.json aside unread, and starts again. Nothing here reads, prints or copies a key.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// rotateInput is where rotate-identity reads the person's yes; tests replace it.
var rotateInput io.Reader = os.Stdin

// rotateStartWait is how long rotate-identity waits for serve to answer again with its new identity.
var rotateStartWait = time.Minute

func rotateIdentity(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("rotate-identity", stderr)
	yes := fs.Bool("yes", false, "don't ask before unpairing every phone")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	dir := st.Dir()
	if !tailcat.HasIdentity(dir) {
		fmt.Fprintln(stdout, "this computer has no identity yet (no phone has paired), so there is nothing to replace")
		return nil
	}
	ds, err := st.Devices()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "This gives this computer new keys. All %d paired phone(s) are unpaired and pair again with herdr-anywhere pair;\n", len(ds))
	fmt.Fprintln(stdout, "phones connected now are told to pair again, the others say they can't reach this computer.")
	if !*yes {
		fmt.Fprint(stdout, "Go on? [y/N] ")
		line, _ := bufio.NewReader(rotateInput).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(stdout, "nothing changed")
			return nil
		}
	}
	var retired string
	if tailcat.ServeRunning(dir) {
		ctx, cancel := context.WithTimeout(context.Background(), rotateWait+30*time.Second)
		retired, err = tailcat.RequestRotate(ctx, dir)
		cancel()
		if err != nil {
			return fmt.Errorf("serve could not make a new identity: %w", err)
		}
		if err := waitRotated(dir, retired); err != nil {
			return err
		}
	} else {
		// serve records its pid before it reads tailcat.json and answers on its socket only after its host started: a
		// serve between the two holds the old identity and would save it again.
		if starting(dir) {
			return errors.New("Companion is starting; run rotate-identity again in a moment")
		}
		if err := revokeAll(dir); err != nil {
			return err
		}
		retired = tailcat.RetiredName(time.Now())
		if err := tailcat.Retire(dir, retired); err != nil {
			return err
		}
		if starting(dir) || tailcat.ServeRunning(dir) {
			return fmt.Errorf("Companion started meanwhile and may hold the old identity: run herdr-anywhere stop, then rotate-identity again (the old keys are in %s)", filepath.Join(dir, retired))
		}
	}
	fmt.Fprintln(stdout, "This computer has a new identity, and no phone is paired.")
	fmt.Fprintln(stdout, "Pair each phone again: herdr-anywhere pair, or Add phone in Herdr ("+dialogKey+").")
	fmt.Fprintf(stdout, "The old keys are in %s, unread; delete that file once your phones are paired again.\n", filepath.Join(dir, retired))
	return nil
}

// starting: a serve recorded its pid in dir and still runs.
func starting(dir string) bool {
	pid := recordedPID(dir)
	return pid > 0 && pid != os.Getpid() && alive(pid)
}

// revokeAll unpairs every phone paired on dir.
func revokeAll(dir string) error {
	st, err := openStore(dir)
	if err != nil {
		return err
	}
	ds, err := st.Devices()
	if err != nil {
		return err
	}
	for _, d := range ds {
		if err := st.Revoke(d.ID); err != nil && !errors.Is(err, store.ErrNoDevice) {
			return fmt.Errorf("unpairing %s: %w", d.ID, err)
		}
	}
	return nil
}

// waitRotated waits for serve to have moved its old identity to retired and to answer again, as the new process.
// The old serve's control socket closes before the file moves, so a serve answering after that is the new one.
func waitRotated(dir, retired string) error {
	deadline := time.Now().Add(rotateStartWait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, retired)); err == nil && tailcat.ServeRunning(dir) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("serve did not start again with its new identity within %s; see %s", rotateStartWait, filepath.Join(dir, serveLogFile))
}

// rotateRestart ends serve so that main moves its identity to retired, then starts serve again with a new one.
type rotateRestart struct{ dir, retired string }

func (rotateRestart) Error() string { return "restarting with a new identity" }

// rotateWait is how long serve waits after unpairing every phone before it stops: Watch closes a connected phone's
// line with 4401, which the app says as "Pair it again", within a second.
var rotateWait = 3 * time.Second

// rotateHandler is serve's POST /rotate (rotate-identity): every paired phone unpaired while the old identity still
// reaches them, then the name its keys go to, sent on rotating once the answer is out. No key is read or written here.
func rotateHandler(st *store.Store, rotating chan<- string, logger *log.Logger) http.HandlerFunc {
	var busy atomic.Bool
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !tailcat.HasIdentity(st.Dir()) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no identity yet: no phone has paired with this computer"})
			return
		}
		if !busy.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "already making a new identity"})
			return
		}
		ds, err := st.Devices()
		if err == nil {
			for _, d := range ds {
				if err = st.Revoke(d.ID); err != nil && !errors.Is(err, store.ErrNoDevice) {
					break
				}
				err = nil
			}
		}
		if err != nil {
			busy.Store(false)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unpairing the phones: " + err.Error()})
			return
		}
		logger.Printf("rotate-identity: unpaired %d phone(s); a new identity after the restart", len(ds))
		time.Sleep(rotateWait)
		name := tailcat.RetiredName(time.Now())
		_ = json.NewEncoder(w).Encode(map[string]string{"retired": name})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		rotating <- name
	}
}
