package main

// language: which of the phone app's seven languages this computer uses, for the Add phone dialog.
// Herdr runs the dialog in QuickJS, which has no Intl and no way to read the system's language, so
// the dialog runs this. Each system says it in its own place:
//   - macOS: the Language & Region list, `defaults read -g AppleLanguages`; LANG is a shell's, not the Mac's.
//   - Windows: the display languages, GetUserPreferredUILanguages.
//   - Linux and the rest: gettext's variables, LANGUAGE (a list) then LC_ALL, LC_MESSAGES, LANG.
// The first one of the seven in the list wins, as an app picks among its translations; none is English.
// HERDR_ANYWHERE_LANG overrides all of them.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// dialogLangs are the phone app's languages (app/src/strings/index.ts) and the dialog's (dialog/strings.js).
var dialogLangs = []string{"en", "zh-Hans", "zh-Hant", "es", "ko", "ja", "ru"}

// langFor is the phone app's langFor for a BCP 47 tag or a POSIX locale, except that a language
// without a translation is "", not English, so a list can go on to the next one.
func langFor(tag string) (string, bool) {
	t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	t, _, _ = strings.Cut(t, ".") // ja_JP.UTF-8
	t, _, _ = strings.Cut(t, "@") // sr_RS@latin
	parts := strings.Split(t, "-")
	has := func(p string) bool {
		for _, x := range parts[1:] {
			if x == p {
				return true
			}
		}
		return false
	}
	switch parts[0] {
	case "zh":
		if has("hant") || (!has("hans") && (has("tw") || has("hk") || has("mo"))) {
			return "zh-Hant", true
		}
		return "zh-Hans", true
	case "en", "es", "ko", "ja", "ru":
		return parts[0], true
	}
	return "", false
}

type langSources struct {
	getenv             func(string) string
	appleLanguages     func() ([]byte, error)
	windowsUILanguages func() ([]string, error)
}

func detectLanguage(goos string, src langSources) string {
	if v := strings.TrimSpace(src.getenv("HERDR_ANYWHERE_LANG")); v != "" {
		if l, ok := langFor(v); ok {
			return l
		}
		return "en"
	}
	var tags []string
	switch goos {
	case "darwin":
		if b, err := src.appleLanguages(); err == nil {
			tags = appleLanguageList(b)
		}
	case "windows":
		tags, _ = src.windowsUILanguages()
	default:
		tags = posixLanguages(src.getenv)
	}
	for _, tag := range tags {
		if l, ok := langFor(tag); ok {
			return l
		}
	}
	return "en"
}

var quoted = regexp.MustCompile(`"([^"]+)"|([A-Za-z][A-Za-z0-9_-]*)`)

// appleLanguageList reads what `defaults read -g AppleLanguages` prints: ( "en-US", "zh-Hans-US" ).
// A tag without a region (ja) comes without quotes.
func appleLanguageList(b []byte) []string {
	var tags []string
	for _, m := range quoted.FindAllStringSubmatch(string(b), -1) {
		tags = append(tags, m[1]+m[2])
	}
	return tags
}

// posixLanguages follows gettext: the locale is LC_ALL, else LC_MESSAGES, else LANG; when that is C
// (or unset), messages are untranslated and LANGUAGE is ignored.
func posixLanguages(getenv func(string) string) []string {
	locale := ""
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(k); v != "" {
			locale = v
			break
		}
	}
	if base, _, _ := strings.Cut(locale, "."); base == "" || base == "C" || base == "POSIX" {
		return nil
	}
	var tags []string
	for _, l := range strings.Split(getenv("LANGUAGE"), ":") {
		if l != "" {
			tags = append(tags, l)
		}
	}
	return append(tags, locale)
}

func readAppleLanguages() ([]byte, error) {
	return exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
}

func systemLanguage() string {
	return detectLanguage(runtime.GOOS, langSources{getenv: os.Getenv, appleLanguages: readAppleLanguages, windowsUILanguages: windowsUILanguages})
}

func languageCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("herdr-anywhere language", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	fmt.Fprintln(stdout, systemLanguage())
	return nil
}
