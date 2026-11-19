//go:build tools

// Package tools pins the gomobile toolchain so scripts/build-mobile.sh builds it from this module.
package tools

import (
	_ "golang.org/x/mobile/cmd/gobind"
	_ "golang.org/x/mobile/cmd/gomobile"
)
