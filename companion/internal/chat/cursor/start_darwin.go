package cursor

import (
	"time"

	"golang.org/x/sys/unix"
)

func processStarted(pid int) time.Time {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || p.Proc.P_starttime.Sec <= 0 {
		return time.Time{}
	}
	return time.Unix(p.Proc.P_starttime.Sec, int64(p.Proc.P_starttime.Usec)*1000)
}
