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
