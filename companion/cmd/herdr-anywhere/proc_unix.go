//go:build !windows

package main

import (
	"os"
	"syscall"
)

// detached puts a started serve in a session of its own, so it outlives whoever started it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// execSelf replaces this process with exe, keeping its pid, arguments and environment.
func execSelf(exe string) error { return syscall.Exec(exe, os.Args, os.Environ()) }

// endProcess asks pid to end, as Ctrl-C would.
func endProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// alive says whether pid is still running.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// detachConsole: a serve on macOS and Linux has no console window to leave.
func detachConsole() {}
