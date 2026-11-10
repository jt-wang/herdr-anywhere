package main

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// The same tags give the same language as the phone app's langFor (app/src/strings/index.ts).
func TestLangForMatchesThePhoneApp(t *testing.T) {
	for tag, want := range map[string]string{
		"en-US": "en", "ja-JP": "ja", "ko-KR": "ko", "es-419": "es", "ru-RU": "ru",
		"zh-Hans-US": "zh-Hans", "zh-CN": "zh-Hans", "zh": "zh-Hans", "zh-Hant-TW": "zh-Hant", "zh-HK": "zh-Hant",
		"zh-Hans-HK": "zh-Hans", "zh_TW.UTF-8": "zh-Hant", "ja_JP.UTF-8": "ja", "pt-BR": "", "sr_RS@latin": "", "": "",
	} {
		if got, _ := langFor(tag); got != want {
			t.Errorf("langFor(%q) = %q, want %q", tag, got, want)
		}
	}
}

// What `defaults read -g AppleLanguages` printed on a Mac set to French, then Japanese.
const frenchThenJapanese = "(\n    \"fr-FR\",\n    \"ja-JP\",\n    \"en-US\"\n)\n"

func TestMacReadsAppleLanguagesInOrder(t *testing.T) {
	src := langSources{getenv: env(nil), appleLanguages: func() ([]byte, error) { return []byte(frenchThenJapanese), nil }}
	if got := detectLanguage("darwin", src); got != "ja" {
		t.Fatalf("got %q, want ja: French isn't one of the seven, so the next one counts", got)
	}
	// LANG from a shell says nothing about the Mac's language.
	src.getenv = env(map[string]string{"LANG": "ko_KR.UTF-8"})
	if got := detectLanguage("darwin", src); got != "ja" {
		t.Fatalf("with LANG=ko_KR got %q, want ja", got)
	}
	// A tag without a region comes unquoted: `defaults write <domain> AppleLanguages -array ja fr-FR zh-Hant-TW`
	// read back with `defaults read`.
	src.appleLanguages = func() ([]byte, error) { return []byte("(\n    ja,\n    \"fr-FR\",\n    \"zh-Hant-TW\"\n)\n"), nil }
	if got := appleLanguageList(must(src.appleLanguages())); strings.Join(got, " ") != "ja fr-FR zh-Hant-TW" {
		t.Fatalf("read %q", got)
	}
	src.appleLanguages = func() ([]byte, error) { return nil, errors.New("exit status 1") }
	if got := detectLanguage("darwin", src); got != "en" {
		t.Fatalf("without AppleLanguages got %q, want en", got)
	}
}

func TestWindowsReadsThePreferredUILanguages(t *testing.T) {
	src := langSources{getenv: env(map[string]string{"LANG": "ja_JP.UTF-8"}), windowsUILanguages: func() ([]string, error) { return []string{"de-DE", "zh-TW", "en-US"}, nil }}
	if got := detectLanguage("windows", src); got != "zh-Hant" {
		t.Fatalf("got %q, want zh-Hant", got)
	}
	src.windowsUILanguages = func() ([]string, error) { return nil, errors.New("no") }
	if got := detectLanguage("windows", src); got != "en" {
		t.Fatalf("on a failed call got %q, want en", got)
	}
}

// Linux reads the variables gettext reads, in its order: LANGUAGE (a list) only when the locale isn't C.
func TestLinuxReadsTheLocaleVariablesLikeGettext(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LANG": "ja_JP.UTF-8"}, "ja"},
		{map[string]string{"LANG": "en_US.UTF-8", "LC_MESSAGES": "ru_RU.UTF-8"}, "ru"},
		{map[string]string{"LANG": "en_US.UTF-8", "LC_MESSAGES": "ru_RU.UTF-8", "LC_ALL": "ko_KR.UTF-8"}, "ko"},
		{map[string]string{"LANG": "de_DE.UTF-8", "LANGUAGE": "de:zh_TW:en"}, "zh-Hant"},
		{map[string]string{"LANG": "C", "LANGUAGE": "ja"}, "en"},
		{map[string]string{"LANG": "C.UTF-8"}, "en"},
		{map[string]string{"LANGUAGE": "ja"}, "en"},
		{map[string]string{"LANG": "pt_BR.UTF-8"}, "en"},
	} {
		src := langSources{getenv: env(c.env)}
		if got := detectLanguage("linux", src); got != c.want {
			t.Errorf("%v: got %q, want %q", c.env, got, c.want)
		}
	}
}

// HERDR_ANYWHERE_LANG picks one on any computer, over what the system says.
func TestTheVariableOverridesTheSystem(t *testing.T) {
	src := langSources{
		getenv:             env(map[string]string{"HERDR_ANYWHERE_LANG": "ko", "LANG": "ja_JP.UTF-8"}),
		appleLanguages:     func() ([]byte, error) { return []byte(frenchThenJapanese), nil },
		windowsUILanguages: func() ([]string, error) { return []string{"ja-JP"}, nil },
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if got := detectLanguage(goos, src); got != "ko" {
			t.Errorf("%s: got %q, want ko", goos, got)
		}
	}
}

func TestLanguageCommandPrintsOneOfTheSeven(t *testing.T) {
	t.Setenv("HERDR_ANYWHERE_LANG", "zh-Hant")
	var out, errb bytes.Buffer
	if code := run([]string{"language"}, &out, &errb); code != 0 || out.String() != "zh-Hant\n" {
		t.Fatalf("code %d, out %q, err %q", code, out.String(), errb.String())
	}
	if code := run([]string{"language", "extra"}, &out, &errb); code != 2 {
		t.Fatalf("an argument: code %d", code)
	}
}

// On this computer, what the system reader finds: something, and one of the seven.
func TestTheSystemOnThisComputerGivesALanguage(t *testing.T) {
	if runtime.GOOS == "darwin" {
		b, err := readAppleLanguages()
		if err != nil || len(appleLanguageList(b)) == 0 {
			t.Fatalf("defaults read -g AppleLanguages: %v, %q", err, b)
		}
	}
	if runtime.GOOS == "windows" {
		tags, err := windowsUILanguages()
		if err != nil || len(tags) == 0 {
			t.Fatalf("GetUserPreferredUILanguages: %v, %v", err, tags)
		}
	}
	t.Setenv("HERDR_ANYWHERE_LANG", "")
	got := systemLanguage()
	if !strings.Contains(" "+strings.Join(dialogLangs, " ")+" ", " "+got+" ") {
		t.Fatalf("systemLanguage() = %q", got)
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
