package testfake

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise both packaging scripts, replacing only their build tools with Go fakes. The iOS fixture must use the
// same checked NativeShell as the app; nsh package would silently copy its own prebuilt runtime instead.
func TestZoomPackageRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the packaging scripts run on macOS and Linux")
	}
	for _, target := range []string{"ios-simulator", "android", "stale-ios", "plain-ios"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			app := filepath.Join(root, "app")
			write := func(name, value string) {
				t.Helper()
				p := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(value), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"e2e/zoom-package.sh", "ios/build.sh"} {
				b, err := os.ReadFile(filepath.Join("../../../app", name))
				if err != nil {
					t.Fatal(err)
				}
				write("app/"+name, string(b))
			}
			write("app/assets/sounds/done.mp3", "fake sound")
			write("app/ios/build/derived/Build/Products/Release-iphonesimulator/Herdr.app/runtime", "patched engine")
			write("framework/NativeShell.xcframework/runtime", "patched engine")
			write("framework/nsh-build.txt", "patched-test-engine\n")
			write("out/Herdr zoom check.app/runtime", "old CLI engine")
			write("out/zoom.apk", "Android fixture")
			if err := os.MkdirAll(filepath.Join(root, "third_party/tailpocket-32c50f3/sdk/apple/NSH"), 0o755); err != nil {
				t.Fatal(err)
			}
			HC(t, filepath.Join(app, "ios/nsh-patches"), "key.sh", Spec{On: map[string][]Reply{"": {{Print: "patched-test-engine"}}}})
			HC(t, filepath.Join(app, "scripts"), "tailpocket.sh", Spec{})
			bin := filepath.Join(root, "bin")
			calls := filepath.Join(root, "xcode-calls")
			HC(t, bin, "xcodebuild", Spec{Calls: calls})
			HC(t, bin, "xcodegen", Spec{})
			nshCalls := filepath.Join(root, "nsh-calls")
			HC(t, bin, "nsh", Spec{Calls: nshCalls})
			arg := target
			if target == "stale-ios" {
				arg = "ios-simulator"
				write("framework/nsh-build.txt", "old-engine\n")
			}
			cmd := exec.Command("sh", filepath.Join(app, "e2e/zoom-package.sh"), arg, filepath.Join(root, "out"))
			if target == "plain-ios" {
				cmd = exec.Command("sh", filepath.Join(app, "ios/build.sh"), "App.tsx", "simulator")
			}
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "NSH_IOS_EMBED="+filepath.Join(root, "framework"), "NSH_IOS_APP_ID=", "CHAT=1")
			out, err := cmd.CombinedOutput()
			xcode, _ := os.ReadFile(calls)
			nsh, _ := os.ReadFile(nshCalls)
			if target == "stale-ios" {
				if err == nil || !strings.Contains(string(out), "no NativeShell built as") || len(xcode) != 0 {
					t.Fatalf("stale framework was not rejected before building: %v, %s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("package: %v\n%s", err, out)
			}
			if target == "plain-ios" {
				if len(xcode) == 0 || strings.Contains(string(xcode), "PRODUCT_BUNDLE_IDENTIFIER=") {
					t.Fatalf("the ordinary app must keep the project's bundle id: %s", xcode)
				}
				return
			}
			if target == "android" {
				if !strings.Contains(string(nsh), "package --target android") || len(xcode) != 0 {
					t.Fatalf("Android changed build path: nsh=%s xcode=%s", nsh, xcode)
				}
				return
			}
			got, _ := os.ReadFile(filepath.Join(root, "out/Herdr zoom check.app/runtime"))
			if string(got) != "patched engine" || !strings.Contains(string(xcode), "PRODUCT_BUNDLE_IDENTIFIER=io.jingtao.herdranywhere.zoomcheck") {
				t.Fatalf("iOS did not use the app build with its own fixture id: runtime=%q xcode=%s", got, xcode)
			}
			if strings.Contains(string(nsh), "package ") || !strings.Contains(string(nsh), "build .zoom-check.tsx") {
				t.Fatalf("iOS used the CLI runtime: %s", nsh)
			}
			if _, err := os.Stat(filepath.Join(app, ".zoom-check.tsx")); !os.IsNotExist(err) {
				t.Fatal("temporary entry left behind")
			}
		})
	}
}
