package pi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func TestClearedConfirmationKeepsAgentChecks(t *testing.T) {
	s, file := setup(t)
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("// edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, token, err := s.Confirmation()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(s.Dir); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyConfirmed(true, token); !errors.Is(err, chat.ErrNoAgent) {
		t.Fatalf("missing agent: %v", err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatalf("recreated agent directory: %v", err)
	}
	if err := s.ApplyConfirmed(false, token); err != nil {
		t.Fatalf("remove absent agent: %v", err)
	}
}

func TestClearedConfirmationRechecksNewTargets(t *testing.T) {
	for _, install := range []bool{true, false} {
		t.Run(map[bool]string{true: "install", false: "remove"}[install], func(t *testing.T) {
			base, _ := setup(t)
			if err := base.Install(false); err != nil {
				t.Fatal(err)
			}
			added, file := setup(t)
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("// new edit\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s := &Setup{targets: func() ([]*Setup, error) {
				calls++
				if calls == 1 {
					return []*Setup{base}, nil
				}
				return []*Setup{base, added}, nil
			}}
			if err := s.ApplyConfirmed(install, "previous-token"); !errors.Is(err, chat.ErrChanged) {
				t.Fatalf("new conflict: %v", err)
			}
			if b, err := os.ReadFile(file); err != nil || string(b) != "// new edit\n" {
				t.Fatalf("new conflict overwritten: %q %v", b, err)
			}
			if base.State() != chat.Installed {
				t.Fatal("non-forced preflight changed the original target")
			}
		})
	}
}
