package main

// A Tailcat invite is about 600 characters, so its QR code takes about 89 columns × 45 rows of a terminal, more
// than a laptop's terminal shows at once. When it doesn't fit, pair writes the code as an image and opens it.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/term"
	"rsc.io/qr"

	"herdr-anywhere/companion/internal/tailcat"
)

// pairImageFile is the QR image pair writes in the state dir when the terminal is too small for the code.
const pairImageFile = "pair-qr.png"

// errNoScreen: this session has no screen to show an image on (ssh, or Linux with no display).
var errNoScreen = errors.New("no screen to show the image on")

// qrQuietZone is the white border of the terminal code, in modules.
const qrQuietZone = 2

// terminalSize is w's size in character cells; ok is false when w is not a terminal.
var terminalSize = func(w any) (cols, rows int, ok bool) {
	f, isFile := w.(*os.File)
	if !isFile || !term.IsTerminal(int(f.Fd())) {
		return 0, 0, false
	}
	cols, rows, err := term.GetSize(int(f.Fd()))
	return cols, rows, err == nil && cols > 0 && rows > 0
}

// openImage shows the image at path on this computer's screen.
var openImage = func(path string) error {
	argv, err := imageOpener(runtime.GOOS, os.Getenv, exec.LookPath, path)
	if err != nil {
		return err
	}
	return exec.Command(argv[0], argv[1:]...).Run()
}

// imageOpener is the command that shows path in the system's image viewer. Over ssh a Mac or Windows viewer would open
// on the far computer's screen, not the one the person sits at; on Linux, DISPLAY is that screen (X forwarding too).
func imageOpener(goos string, getenv func(string) string, lookPath func(string) (string, error), path string) ([]string, error) {
	overSSH := getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
	switch goos {
	case "darwin":
		if !overSSH {
			return []string{"open", path}, nil
		}
	case "windows":
		if !overSSH {
			return []string{"rundll32.exe", "url.dll,FileProtocolHandler", path}, nil
		}
	default:
		if getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != "" {
			if _, err := lookPath("xdg-open"); err == nil {
				return []string{"xdg-open", path}, nil
			}
		}
	}
	return nil, errNoScreen
}

// qrFootprint is how many columns and rows printQR takes for text: two modules to a row (half blocks), with the quiet zone.
func qrFootprint(text string) (cols, rows int) {
	c, err := qr.Encode(text, qr.L)
	if err != nil {
		return 0, 0
	}
	side := c.Size + 2*qrQuietZone
	return side, (side + 1) / 2
}

// writeQRImage writes text's QR code as a PNG only this user can read (an invite is a pairing secret until it is used
// or expires): 10 pixels a module and the standard four-module border.
func writeQRImage(path, text string) error {
	c, err := qr.Encode(text, qr.L)
	if err != nil {
		return err
	}
	const scale, border = 10, 4
	side := (c.Size + 2*border) * scale
	img := image.NewGray(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			v := uint8(0xff)
			if c.Black(x/scale-border, y/scale-border) {
				v = 0
			}
			img.SetGray(x, y, color.Gray{v})
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// showQR prints text's QR code when the terminal shows all of it with room for after more lines below, and says
// where else to find it when not. Not a terminal (a pipe, a log): the code is printed, as there is no size to go by.
// It removes an image an earlier pair left.
func showQR(w io.Writer, text, dir string, after int) {
	img := filepath.Join(dir, pairImageFile)
	_ = os.Remove(img)
	cols, rows := qrFootprint(text)
	tc, tr, isTerm := terminalSize(w)
	if !isTerm || (cols <= tc && rows+after <= tr) {
		printQR(w, text)
		return
	}
	size := fmt.Sprintf("it needs a terminal of %d columns × %d rows, this one is %d × %d", cols, rows+after, tc, tr)
	if err := writeQRImage(img, text); err != nil {
		fmt.Fprintf(w, "The QR code doesn't fit here (%s), and saving it as an image failed: %v.\nMake the terminal bigger or its font smaller and run pair again, or paste the invite or link shown here into the app.\n", size, err)
		return
	}
	if err := openImage(img); err == nil {
		fmt.Fprintf(w, "The QR code doesn't fit here (%s), so it is open in your image viewer; scan it there:\n  %s\n", size, img)
		return
	} else if !errors.Is(err, errNoScreen) {
		fmt.Fprintf(w, "The QR code doesn't fit here (%s), and opening it as an image failed: %v.\nOpen it yourself:\n  %s\n", size, err, img)
		return
	}
	fmt.Fprintf(w, "The QR code doesn't fit here (%s), and this session has no screen to show an image on.\nIt is saved as an image; open it on a screen:\n  %s\nOr make the terminal bigger or its font smaller and run pair again, or paste the invite or link shown here into the app.\n", size, img)
}

// removeImageAfter is open, removing the image pair writes in dir when the window open gave closes, unless a later
// window opened since: that image is the later pair's.
func removeImageAfter(dir string, open func(context.Context, string) (tailcat.Window, error)) func(context.Context, string) (tailcat.Window, error) {
	var mu sync.Mutex
	var latest int
	return func(ctx context.Context, code string) (tailcat.Window, error) {
		w, err := open(ctx, code)
		if err != nil {
			return w, err
		}
		mu.Lock()
		latest++
		mine := latest
		mu.Unlock()
		time.AfterFunc(time.Until(w.Expires), func() {
			mu.Lock()
			defer mu.Unlock()
			if latest == mine {
				_ = os.Remove(filepath.Join(dir, pairImageFile))
			}
		})
		return w, nil
	}
}
