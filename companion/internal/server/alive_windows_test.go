package server

import "golang.org/x/sys/windows"

// stillActive is STILL_ACTIVE, the exit code of a process that hasn't exited.
const stillActive = 259

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}
