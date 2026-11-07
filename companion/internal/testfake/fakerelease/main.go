// fakerelease stands in for a released companion in self-update tests, on every OS: `--version` prints
// "herdr-anywhere <version>" (or exits with code exit, when set), and `wait` keeps it running, as a serve does.
// version and exit are set when it is built (-ldflags -X).
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

var version, exit string

func main() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case "--version":
		if exit != "" {
			code, _ := strconv.Atoi(exit)
			os.Exit(code)
		}
		fmt.Println("herdr-anywhere " + version)
	case "wait":
		time.Sleep(time.Hour)
	}
}
