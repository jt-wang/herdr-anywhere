package cursor

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func processStarted(pid int) time.Time {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return time.Time{}
	}
	auxv, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return time.Time{}
	}
	now := time.Now()
	var boot unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot) != nil {
		return time.Time{}
	}
	return linuxStarted(string(stat), auxv, now, time.Duration(boot.Nano()))
}

func linuxStarted(stat string, auxv []byte, now time.Time, uptime time.Duration) time.Time {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 20 {
		return time.Time{}
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return time.Time{}
	}
	word := strconv.IntSize / 8
	read := func(b []byte) uint64 {
		if word == 4 {
			return uint64(binary.NativeEndian.Uint32(b))
		}
		return binary.NativeEndian.Uint64(b)
	}
	const atClockTicks = 17 // AT_CLKTCK
	var hz uint64
	for len(auxv) >= 2*word {
		if read(auxv) == atClockTicks {
			hz = read(auxv[word:])
			break
		}
		auxv = auxv[2*word:]
	}
	if hz == 0 || ticks/hz > uint64(uptime/time.Second) {
		return time.Time{}
	}
	elapsed := time.Duration(ticks/hz)*time.Second + time.Duration(ticks%hz)*time.Second/time.Duration(hz)
	if elapsed > uptime {
		return time.Time{}
	}
	return now.Add(elapsed - uptime)
}
