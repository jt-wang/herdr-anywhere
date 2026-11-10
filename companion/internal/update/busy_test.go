package update

import (
	"errors"
	"testing"
	"time"
)

var errHeld = errors.New("held by another process")

func isHeld(err error) bool { return errors.Is(err, errHeld) }

// Windows on Arm holds an x64 program's file a moment after it ran: the rename that puts the new file in place fails
// a few times, then goes through.
func TestWhileBusyWaitsOutAFileHeldAMoment(t *testing.T) {
	calls := 0
	err := whileBusy(func() error {
		if calls++; calls < 4 {
			return errHeld
		}
		return nil
	}, isHeld, 5*time.Second)
	if err != nil || calls != 4 {
		t.Fatalf("err %v after %d tries, want nil after 4", err, calls)
	}
}

func TestWhileBusyDoesNotRetryOtherErrors(t *testing.T) {
	calls, other := 0, errors.New("no such file")
	if err := whileBusy(func() error { calls++; return other }, isHeld, 5*time.Second); err != other || calls != 1 {
		t.Fatalf("err %v after %d tries, want the error after 1", err, calls)
	}
}

func TestWhileBusyGivesUp(t *testing.T) {
	start := time.Now()
	if err := whileBusy(func() error { return errHeld }, isHeld, 300*time.Millisecond); !errors.Is(err, errHeld) {
		t.Fatalf("err %v, want the busy error once the wait is over", err)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 2*time.Second {
		t.Fatalf("gave up after %v, want about 300ms", d)
	}
}
