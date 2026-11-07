package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"herdr-anywhere/companion/internal/update"
)

// errRestart ends serve so that main can run the new file in its place.
var errRestart = errors.New("restarting with the new version")

// updates joins update.Manager to the server: the serve command supplies how to restart.
type updates struct {
	*update.Manager
	restart func()
}

func (u updates) Restart()         { u.restart() }
func (u updates) CanInstall() bool { return u.Installs }

// thisFile is the file to replace: the real one, when it was started through a link.
func thisFile() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// releaseFlags lets tests point at releases of their own, signed with a key of their own.
func releaseFlags(fs *flag.FlagSet) func() (update.Client, error) {
	url := fs.String("releases", "", "where releases are (for tests; default this repo on GitHub)")
	key := fs.String("release-key", "", "the base64 Ed25519 key releases are signed with (for tests; default the built-in one)")
	return func() (update.Client, error) {
		c := update.Client{Releases: *url, Current: version}
		if *key != "" {
			b, err := base64.StdEncoding.DecodeString(*key)
			if err != nil || len(b) != ed25519.PublicKeySize {
				return c, errors.New("--release-key is not a base64 Ed25519 public key")
			}
			c.Key = ed25519.PublicKey(b)
		}
		return c, nil
	}
}

func newUpdates(dir string, client update.Client, logger *log.Logger, restart func()) updates {
	exe, err := thisFile()
	if err != nil {
		logger.Printf("update: can't find this program: %v", err)
	} else {
		go update.RemoveOld(exe) // the file an update set aside on Windows
	}
	first := 30 * time.Second // a serve that starts and stops at once asks nothing
	if client.Releases != "" {
		first = 0
	}
	return updates{Manager: &update.Manager{
		Client:   client,
		Exe:      exe,
		Installs: err == nil && update.Supported(),
		Dir:      dir,
		First:    first,
		Every:    24 * time.Hour,
		Restart:  restart,
		Logf:     logger.Printf,
	}, restart: restart}
}

// restartSelf runs the file now at this program's path, as this same process with these same arguments: whatever
// started serve, a terminal or the Herdr dialog, keeps watching it, and phones connect again by themselves.
func restartSelf() error {
	exe, err := thisFile()
	if err != nil {
		return err
	}
	return execSelf(exe)
}

func updateCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, _ := newFlags("update", stderr)
	check := fs.Bool("check", false, "only say whether there is a new version")
	releases := releaseFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	c, err := releases()
	if err != nil {
		return err
	}
	rel, err := c.Latest(ctx)
	if err != nil {
		return err
	}
	if !update.Newer(rel.Version, version) {
		fmt.Fprintf(stdout, "herdr-anywhere %s is the latest version\n", version)
		return nil
	}
	if *check {
		fmt.Fprintf(stdout, "herdr-anywhere %s is available (this is %s); install it with: herdr-anywhere update\n", rel.Version, version)
		return nil
	}
	if !update.Supported() {
		return update.ErrNotHere
	}
	exe, err := thisFile()
	if err != nil {
		return err
	}
	if err := c.Apply(ctx, rel, exe); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "updated to herdr-anywhere %s; a serve that is running keeps the old version until it restarts\n", rel.Version)
	return nil
}
