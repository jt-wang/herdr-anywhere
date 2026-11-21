//go:build !linux

package main

import "testing"

// noTailcatSockets needs /proc (Linux); elsewhere TestLiveLANOnly checks what serve and the phone report.
func noTailcatSockets(*testing.T) func() { return func() {} }
