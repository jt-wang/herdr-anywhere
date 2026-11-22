package pi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"herdr-anywhere/companion/internal/chat"
)

var ompBlobRef = regexp.MustCompile(`^blob:sha256:([a-f0-9]{64})$`)

func (r *Reader) imageData(data string) ([]byte, error) {
	if !strings.HasPrefix(data, "blob:") {
		return base64.StdEncoding.DecodeString(data)
	}
	m := ompBlobRef.FindStringSubmatch(data)
	if m == nil || r.Setup == nil || r.Setup.Agent() != "omp" {
		return nil, chat.ErrNoStep
	}
	for _, dir := range r.Setup.roots(true) {
		if b, err := readOMPBlob(dir, m[1]); err == nil {
			return b, nil
		}
	}
	return nil, chat.ErrNoStep
}

func readOMPBlob(dir, hash string) ([]byte, error) {
	f, err := os.Open(filepath.Join(dir, hash))
	if err != nil {
		return nil, chat.ErrNoStep
	}
	defer f.Close()
	const maxImage = 12 << 20
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxImage {
		return nil, chat.ErrNoStep
	}
	b, err := io.ReadAll(io.LimitReader(f, maxImage+1))
	if err != nil || len(b) > maxImage {
		return nil, chat.ErrNoStep
	}
	digest := sha256.Sum256(b)
	if hex.EncodeToString(digest[:]) != hash {
		return nil, chat.ErrNoStep
	}
	return b, nil
}
