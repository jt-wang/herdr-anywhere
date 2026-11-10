//go:build linux

package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

const defaultName = "herdr-anywhere"

func (s *Service) unit() string { return s.Name + ".service" }

// Path is the user unit, in systemd's user directory under XDG_CONFIG_HOME.
func (s *Service) Path() string {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(s.Home, ".config")
	}
	return filepath.Join(config, "systemd", "user", s.unit())
}

// Install writes the unit and enables it, so it starts at each login. Without a systemd user instance (a
// container, an ssh session with no login) the file is written and the error says why it isn't enabled.
func (s *Service) Install(e Entry) (bool, error) {
	changed, err := writeIfChanged(s.Path(), []byte(systemdUnit(e)))
	if err != nil {
		return changed, err
	}
	if _, err := s.command("systemctl", "--user", "daemon-reload"); err != nil {
		return changed, err
	}
	_, err = s.command("systemctl", "--user", "enable", s.unit())
	return changed, err
}

// Start runs it now under systemd.
func (s *Service) Start() error {
	_, err := s.command("systemctl", "--user", "start", s.unit())
	return err
}

// Remove stops and disables it and deletes the unit, and says whether there was one.
func (s *Service) Remove() (bool, error) {
	if _, err := os.Stat(s.Path()); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	_, _ = s.command("systemctl", "--user", "disable", "--now", s.unit())
	if err := os.Remove(s.Path()); err != nil {
		return false, err
	}
	_, _ = s.command("systemctl", "--user", "daemon-reload")
	return true, nil
}
