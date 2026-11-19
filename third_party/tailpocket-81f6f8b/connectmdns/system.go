package connectmdns

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const darwinDNSSD = "/usr/bin/dns-sd"

// SystemAvailable reports whether the OS Bonjour daemon can register services: dns-sd on macOS,
// avahi-publish-service on Linux.
func SystemAvailable() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := os.Stat(darwinDNSSD)
		return err == nil
	case "linux":
		_, err := exec.LookPath("avahi-publish-service")
		return err == nil
	}
	return false
}

// AdvertiseSystem registers s with the OS daemon for as long as the child process runs.
func AdvertiseSystem(s Service) (func(), error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	var ready string
	switch runtime.GOOS {
	case "darwin":
		args := append([]string{"-R", s.Instance, s.Type, "local", fmt.Sprint(s.Port)}, s.TXT...)
		cmd, ready = exec.Command(darwinDNSSD, args...), "Name now registered and active"
	case "linux":
		args := append([]string{"-s", s.Instance, s.Type, fmt.Sprint(s.Port)}, s.TXT...)
		cmd, ready = exec.Command("avahi-publish-service", args...), "Established under name"
	default:
		return nil, errors.New("connectmdns: no system Bonjour tool on " + runtime.GOOS)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}
	registered := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(out)
		said := false
		for sc.Scan() {
			if !said && strings.Contains(sc.Text(), ready) {
				said = true
				registered <- true
			}
		}
		if !said {
			registered <- false
		}
	}()
	select {
	case ok := <-registered:
		if !ok {
			stop()
			return nil, errors.New("connectmdns: system registration failed")
		}
	case <-time.After(5 * time.Second):
		// Some builds buffer their output; registration usually still succeeded.
	}
	var once bool
	return func() {
		if !once {
			once = true
			stop()
		}
	}, nil
}
