//go:build darwin

package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

const defaultName = "io.jingtao.herdranywhere.companion"

// Path is the LaunchAgent, which launchd loads at each login.
func (s *Service) Path() string {
	return filepath.Join(s.Home, "Library", "LaunchAgents", s.Name+".plist")
}

func (s *Service) domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the LaunchAgent, replacing an older one; it runs from the next login, or from Start.
func (s *Service) Install(e Entry) (bool, error) {
	if s.refuse != nil {
		return false, s.refuse
	}
	return writeIfChanged(s.Path(), launchdPlist(s.Name, e))
}

// Start loads it now, which runs it (RunAtLoad). A copy loaded before, perhaps of an older file, goes first.
func (s *Service) Start() error {
	if s.refuse != nil {
		return s.refuse
	}
	_, _ = s.command("launchctl", "bootout", s.domain()+"/"+s.Name)
	_, err := s.command("launchctl", "bootstrap", s.domain(), s.Path())
	return err
}

// Remove stops it and deletes the LaunchAgent, and says whether there was one.
func (s *Service) Remove() (bool, error) {
	if s.refuse != nil {
		return false, nil // nothing of this HOME is under the person\'s name
	}
	_, _ = s.command("launchctl", "bootout", s.domain()+"/"+s.Name)
	err := os.Remove(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
