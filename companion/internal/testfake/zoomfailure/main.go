// zoomfailure supplies XCTest's terse log and a result-bundle failure to script tests.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func value(flag string) string {
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func main() {
	if filepath.Base(os.Args[0]) == "xcodebuild" {
		if result := value("-resultBundlePath"); result != "" {
			if err := os.MkdirAll(result, 0700); err != nil {
				panic(err)
			}
		}
		fmt.Println("Failing tests:\n\tZoom.test()\n** TEST FAILED **")
		os.Exit(65)
	}
	if len(os.Args) < 3 || os.Args[1] != "xcresulttool" {
		if len(os.Args) == 6 && os.Args[1] == "simctl" && os.Args[2] == "io" && os.Args[4] == "screenshot" {
			if err := os.WriteFile(os.Args[5], []byte("synthetic screenshot\n"), 0600); err != nil {
				panic(err)
			}
			return
		}
		os.Exit(2)
	}
	switch os.Args[2] {
	case "get":
		fmt.Println(`{"testFailures":[{"testIdentifierString":"Zoom/test()","failureText":"synthetic baseline failure"}]}`)
	case "export":
		out := value("--output-path")
		if err := os.MkdirAll(out, 0700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(out, "assertion.txt"), []byte("synthetic UI state\n"), 0600); err != nil {
			panic(err)
		}
	default:
		os.Exit(2)
	}
}
