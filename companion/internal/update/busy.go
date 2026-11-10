package update

import "time"

// whileBusy runs op until it stops failing with an error busy says is passing, for up to wait.
func whileBusy(op func() error, busy func(error) bool, wait time.Duration) error {
	end := time.Now().Add(wait)
	for {
		err := op()
		if err == nil || !busy(err) || !time.Now().Before(end) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
