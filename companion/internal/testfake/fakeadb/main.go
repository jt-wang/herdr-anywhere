// Command fakeadb stands in for adb in tests of the e2e scripts' helpers (app/e2e/android-lib.sh): it does what the
// JSON file beside it says (see testfake.ADBSpec), counting calls in files beside it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// spec is testfake.ADBSpec.
type spec struct {
	Dumps   []int    `json:"dumps"`
	Screens []string `json:"screens"`
	Calls   string   `json:"calls"`
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
	base := strings.TrimSuffix(exe, ".exe")
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	var s spec
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(98)
	}
	args := strings.Join(os.Args[1:], " ")
	if s.Calls != "" {
		f, err := os.OpenFile(s.Calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, args)
			f.Close()
		}
	}
	switch {
	case strings.HasPrefix(args, "shell uiautomator dump"):
		n := count(base+".dumps", 1)
		if n <= len(s.Dumps) && s.Dumps[n-1] != 0 {
			// As a dump killed on the phone: adb shell passes its exit status on, and the last file stays.
			os.Exit(s.Dumps[n-1])
		}
		count(base+".screen", 1)
	case strings.HasPrefix(args, "shell cat /sdcard/hm.xml"):
		k := count(base+".screen", 0)
		if k == 0 || len(s.Screens) == 0 {
			fmt.Fprintln(os.Stderr, "cat: /sdcard/hm.xml: No such file or directory")
			os.Exit(1)
		}
		if k > len(s.Screens) {
			k = len(s.Screens)
		}
		fmt.Print(s.Screens[k-1])
	}
}

// count adds add to the number in file and returns it.
func count(file string, add int) int {
	b, _ := os.ReadFile(file)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	n += add
	if add != 0 {
		os.WriteFile(file, []byte(strconv.Itoa(n)), 0o644)
	}
	return n
}
