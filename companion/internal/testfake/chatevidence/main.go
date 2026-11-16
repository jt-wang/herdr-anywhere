// chatevidence provides simulator container operations for the cleanup test.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if filepath.Base(os.Args[0]) != "xcrun" {
		return
	}
	if log := os.Getenv("EVIDENCE_TEST_CALLS"); log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
		if err := f.Close(); err != nil {
			panic(err)
		}
	}
	if len(os.Args) < 3 {
		os.Exit(2)
	}
	switch os.Args[2] {
	case "get_app_container":
		fmt.Println(os.Getenv("EVIDENCE_TEST_CONTAINER"))
	case "uninstall":
		if err := os.RemoveAll(os.Getenv("EVIDENCE_TEST_CONTAINER")); err != nil {
			panic(err)
		}
	}
}
