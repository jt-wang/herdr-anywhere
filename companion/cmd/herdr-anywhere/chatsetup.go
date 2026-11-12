package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/hcbin"
)

// chatSetups are the small files some agents' chats need (chat.Setup), by the agent's OSC 7501 app name. serve
// offers them to the phone and the chat-setup command runs them; adding an agent's is one entry here.
func chatSetups(hc string) map[string]chat.Setup {
	return map[string]chat.Setup{
		"pi": &pi.Setup{Dir: pi.AgentDir(), HC: hc},
	}
}

const chatSetupUsage = `usage: herdr-anywhere chat-setup status [agent] [--hc PATH]
       herdr-anywhere chat-setup install|remove <agent> [--force] [--hc PATH]

Some agents' chats on the phone need one small file in the agent's own folder: it tells the terminal which
conversation that agent writes. install puts it there; remove takes it away and leaves nothing behind. A file
someone changed is left as it is unless --force. An agent already running uses it after a restart, or what status
says to type in it.
`

func chatSetupCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, chatSetupUsage)
		if len(args) == 0 {
			return usageError{errors.New("chat-setup needs status, install or remove")}
		}
		return nil
	}
	sub, rest := args[0], args[1:]
	agent := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		agent, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("herdr-anywhere chat-setup "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "replace or remove the file even if someone changed it")
	hcFlag := fs.String("hc", "", "the hc the file calls (default: as serve finds it)")
	if err := parse(fs, rest); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	hc, _, err := hcbin.Resolve(*hcFlag, exec.LookPath, home)
	if err != nil {
		return err
	}
	setups := chatSetups(hc)
	names := slices.Sorted(maps.Keys(setups))
	if agent == "" && sub == "status" {
		for _, n := range names {
			printSetup(stdout, setups[n])
		}
		return nil
	}
	s, ok := setups[agent]
	if !ok {
		return usageError{fmt.Errorf("chat-setup %s needs one of: %s", sub, strings.Join(names, ", "))}
	}
	switch sub {
	case "status":
		printSetup(stdout, s)
		return nil
	case "install":
		err = s.Install(*force)
	case "remove":
		err = s.Remove(*force)
	default:
		return usageError{fmt.Errorf("unknown chat-setup command %q", sub)}
	}
	switch {
	case errors.Is(err, chat.ErrNoAgent):
		return fmt.Errorf("%s isn't on this computer; nothing was made", s.Name())
	case errors.Is(err, chat.ErrChanged):
		return fmt.Errorf("%s was changed by someone and was left as it is; run again with --force to %s it anyway", s.File(), sub)
	case err != nil:
		return err
	}
	if sub == "install" {
		fmt.Fprintf(stdout, "installed: %s\n", s.File())
		if s.Reload() != "" {
			fmt.Fprintf(stdout, "A %s already running uses it after %s in it, or a restart.\n", s.Name(), s.Reload())
		}
	} else {
		fmt.Fprintf(stdout, "removed; nothing of it is left (%s)\n", s.File())
	}
	return nil
}

func printSetup(w io.Writer, s chat.Setup) {
	var say string
	switch s.State() {
	case chat.NoAgent:
		say = "not on this computer"
	case chat.Absent:
		say = "not installed: its chat is off (herdr-anywhere chat-setup install " + s.Agent() + ")"
	case chat.Installed:
		say = "installed: " + s.File()
	case chat.Old:
		say = "installed by an older Companion or for another hc: install again to update it (" + s.File() + ")"
	case chat.Changed:
		say = "changed by someone: " + s.File() + " (install or remove with --force)"
	}
	fmt.Fprintf(w, "%s\t%s\n", s.Agent(), say)
}
