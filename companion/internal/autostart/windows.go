//go:build windows

package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const defaultName = "Herdr Anywhere"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Path names the Run key's value.
func (s *Service) Path() string { return `HKCU\` + runKey + `\` + s.Name }

// Install sets the Run key's value, which Windows runs at each login.
func (s *Service) Install(e Entry) (bool, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	want := windowsCommandLine(e)
	if old, _, err := k.GetStringValue(s.Name); err == nil && old == want {
		return false, nil
	}
	return true, k.SetStringValue(s.Name, want)
}

// Start: Windows has nothing that starts a Run entry now; the caller starts serve itself.
func (s *Service) Start() error { return ErrNoManager }

// Remove deletes the Run key's value, and says whether there was one. A running serve is the caller's to stop.
func (s *Service) Remove() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	err = k.DeleteValue(s.Name)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Value is what the Run key holds now, for tests.
func (s *Service) Value() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(s.Name)
	return v, err
}
