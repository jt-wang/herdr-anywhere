package pi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func TestOMPBlobImages(t *testing.T) {
	dir := "testdata/omp/19.0.0/externalized"
	r := &Reader{Setup: &Setup{ID: "omp", BlobDir: filepath.Join(dir, "blobs")}}
	file := filepath.Join(dir, "images.jsonl")
	ts, err := r.Turns(file)
	if err != nil || len(ts) != 1 || len(ts[0].Images) != 1 {
		t.Fatalf("turns: %+v %v", ts, err)
	}
	d, err := r.Detail(file, "read1")
	if err != nil || len(d.Images) != 1 {
		t.Fatalf("detail: %+v %v", d, err)
	}
	for _, id := range []string{ts[0].Images[0].ID, d.Images[0].ID} {
		media, b, err := r.Image(file, id)
		if err != nil || media != "image/png" || len(b) != 3172 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatalf("image %s: %s %d %v", id, media, len(b), err)
		}
	}
}

func TestOMPBlobRefValidation(t *testing.T) {
	r := &Reader{Setup: &Setup{ID: "omp", BlobDir: t.TempDir()}}
	for _, ref := range []string{"", "blob:sha256:", "blob:sha256:../secret", "blob:sha256:" + strings.Repeat("a", 63), "blob:sha256:" + strings.Repeat("a", 65), "blob:sha256:" + strings.Repeat("A", 64), "blob:sha256:" + strings.Repeat("a", 64) + "/../secret", "blob:sha256:" + strings.Repeat("a", 64)} {
		if _, err := r.imageData(ref); err == nil && ref != "" {
			t.Fatalf("accepted %q", ref)
		}
	}
	hash := strings.Repeat("a", 64)
	path := filepath.Join(r.Setup.BlobDir, hash)
	if err := os.WriteFile(path, []byte("wrong digest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.imageData("blob:sha256:" + hash); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("corrupt blob: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate((12 << 20) + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.imageData("blob:sha256:" + hash); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("oversize blob: %v", err)
	}
	r.Setup.ID = "pi"
	if _, err := r.imageData("blob:sha256:" + hash); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("Pi accepted OMP ref: %v", err)
	}
}
