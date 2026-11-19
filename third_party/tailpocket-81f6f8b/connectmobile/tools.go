//go:build tools

package connectmobile

// gomobile bind needs golang.org/x/mobile/bind in the module's requirements.
import _ "golang.org/x/mobile/bind"
