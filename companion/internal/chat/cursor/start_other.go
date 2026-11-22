//go:build !linux && !darwin

package cursor

import "time"

func processStarted(int) time.Time { return time.Time{} }
