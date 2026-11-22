package pi

import (
	"fmt"
	"regexp"
	"strings"

	"herdr-anywhere/companion/internal/chat"
)

const leafLabel = "herdr-anywhere.chat.leaf"

var leafSnapshot = regexp.MustCompile(`^([A-Za-z0-9_-]{1,128}|~):([A-Za-z0-9_-]{1,128}|~)$`)

// A snapshot pairs the live leaf with the physical journal tail at navigation time.
// Later journal entries can advance that leaf without another extension report.
func selectedPath(file, snapshot string, inode uint64) (string, error) {
	if snapshot == "" {
		return file, nil
	}
	if !leafSnapshot.MatchString(snapshot) {
		return "", &chat.Miss{Reason: chat.NoMatch}
	}
	return fmt.Sprintf("%s\x00%s\x00%d", file, snapshot, inode), nil
}

func recordPath(path string) string {
	file, _, _ := strings.Cut(path, "\x00")
	return file
}

func snapshotOf(path string) (leaf, tail string) {
	_, rest, _ := strings.Cut(path, "\x00")
	snapshot, _, _ := strings.Cut(rest, "\x00")
	if m := leafSnapshot.FindStringSubmatch(snapshot); m != nil {
		return m[1], m[2]
	}
	return "", ""
}
