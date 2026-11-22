package pi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"

	"herdr-anywhere/companion/internal/chat"
)

func (s *Setup) confirmationTargets() ([]*Setup, error) {
	if s.targets != nil {
		return s.targets()
	}
	return []*Setup{s}, nil
}

func (s *Setup) Confirmation() ([]string, string, error) {
	targets, err := s.confirmationTargets()
	if err != nil {
		return nil, "", err
	}
	return confirmationFiles(targets)
}

func confirmationFiles(targets []*Setup) ([]string, string, error) {
	var files []string
	for _, target := range targets {
		if target.State() == chat.Changed {
			files = append(files, target.File())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, "", nil
	}
	h := sha256.New()
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(b)
		encoded, _ := json.Marshal([]string{file, hex.EncodeToString(sum[:])})
		h.Write(encoded)
	}
	return files, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Setup) ApplyConfirmed(install bool, token string) error {
	s.confirmMu.Lock()
	defer s.confirmMu.Unlock()
	targets, err := s.confirmationTargets()
	if err != nil {
		return err
	}
	files, current, err := confirmationFiles(targets)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		if install {
			return s.Install(false)
		}
		return s.Remove(false)
	}
	if token == "" || current != token {
		return chat.ErrChanged
	}
	confirmed := map[string]bool{}
	for _, file := range files {
		confirmed[file] = true
	}
	for _, target := range targets {
		if install {
			if err := os.MkdirAll(target.Dir, 0o755); err != nil {
				return err
			}
		}
		if install {
			err = target.Install(confirmed[target.File()])
		} else {
			err = target.Remove(confirmed[target.File()])
		}
		if err != nil {
			return err
		}
	}
	return nil
}
