//go:build !darwin && !linux && !windows

package autostart

const defaultName = "herdr-anywhere"

func (s *Service) Path() string                { return "" }
func (s *Service) Install(Entry) (bool, error) { return false, ErrNotHere }
func (s *Service) Start() error                { return ErrNotHere }
func (s *Service) Remove() (bool, error)       { return false, nil }
