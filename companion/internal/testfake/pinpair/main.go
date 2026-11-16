// pinpair supplies a virtual build clock and test-app Storage for the pin pairing test.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	root := os.Getenv("PIN_PAIR_TEST_ROOT")
	path := func(name string) string { return filepath.Join(root, name) }
	read := func(name string) string { b, _ := os.ReadFile(path(name)); return string(b) }
	put := func(name, value string) { must(os.WriteFile(path(name), []byte(value), 0600)) }
	exists := func(name string) bool { _, err := os.Stat(path(name)); return err == nil }
	clock, _ := strconv.Atoi(read("clock"))
	container := path("container")
	storage := filepath.Join(container, "Library/Application Support/nsh/sample.app/storage.json")
	switch filepath.Base(os.Args[0]) {
	case "herdr-anywhere":
		if len(os.Args) < 2 || os.Args[1] != "pair" {
			os.Exit(2)
		}
		if exists("installed") {
			put("pair-after-install", "yes")
			if os.Getenv("PIN_PAIR_TEST_FAIL") == "pair" {
				put("pair-failed", "yes")
				os.Exit(17)
			}
		}
		number, _ := strconv.Atoi(read("issued-count"))
		number++
		code := fmt.Sprintf("fixture-%d", number)
		put("issued-count", strconv.Itoa(number))
		put(code, strconv.Itoa(clock+600))
		fmt.Printf("http://127.0.0.1:7991#pair=%s\n", code)
	case "build.sh":
		body := read("src/e2e/pair.ts")
		link := regexp.MustCompile(`http://[^'\s]+#pair=[A-Za-z0-9_-]+`).FindString(body)
		if link == "" {
			fmt.Fprintln(os.Stderr, "no synthetic build pairing link")
			os.Exit(3)
		}
		put("built-link", link)
		delay, err := strconv.Atoi(os.Getenv("PIN_PAIR_TEST_BUILD_SECONDS"))
		must(err)
		put("clock", strconv.Itoa(clock+delay))
	case "xcrun":
		if len(os.Args) < 3 || os.Args[1] != "simctl" {
			os.Exit(2)
		}
		switch os.Args[2] {
		case "boot", "bootstatus":
		case "uninstall":
			must(os.RemoveAll(container))
		case "install":
			put("installed", "yes")
			must(os.MkdirAll(filepath.Dir(storage), 0700))
			if os.Getenv("PIN_PAIR_TEST_FAIL") == "storage" {
				must(os.Mkdir(storage, 0700))
			}
		case "get_app_container":
			fmt.Println(container)
		case "launch":
			put("launched", "yes")
			// The existing ios-e2e entry prefers e2e-pair to its compiled PAIR.
			link := read("built-link")
			if b, err := os.ReadFile(storage); err == nil {
				values := map[string]string{}
				must(json.Unmarshal(b, &values))
				if values["e2e-pair"] != "" {
					link = values["e2e-pair"]
					put("used-storage", "yes")
				}
			}
			parts := strings.Split(link, "#pair=")
			if len(parts) != 2 {
				fmt.Fprintln(os.Stderr, "missing synthetic pairing link")
				os.Exit(4)
			}
			expiry, err := strconv.Atoi(read(parts[1]))
			must(err)
			if clock >= expiry {
				fmt.Fprintln(os.Stderr, "synthetic pairing expired")
				os.Exit(9)
			}
			put("accepted", "yes")
		default:
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
}
