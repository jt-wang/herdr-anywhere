// Package pipefailprobe holds one test for the pipefail probe in tests.yml: it fails when HM_PROBE_FAIL is 1.
package pipefailprobe

import (
	"os"
	"testing"
)

func TestProbe(t *testing.T) {
	if os.Getenv("HM_PROBE_FAIL") == "1" {
		t.Fatal("a failure on purpose: the step must fail with it")
	}
}
