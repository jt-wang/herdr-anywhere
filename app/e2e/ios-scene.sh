#!/bin/sh
# iOS 27 ends the launch of an app built with the iOS 27 SDK that does not use the scene life cycle (crash in
# _UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption). This checks a built Herdr.app for it: Info.plist
# names a scene delegate for the application role and the binary has that class. Runs no compile. Usage: e2e/ios-scene.sh path/to/Herdr.app
set -eu
app=${1:?usage: e2e/ios-scene.sh path/to/Herdr.app}
plist=$app/Info.plist
bin=$app/$(plutil -extract CFBundleExecutable raw "$plist")
fail=0
ok() { echo "✓ $1"; }
bad() { echo "✗ $1"; fail=1; }

if plutil -extract UIApplicationSceneManifest xml1 -o - "$plist" >/dev/null 2>&1; then ok 'Info.plist has UIApplicationSceneManifest'; else bad 'Info.plist has no UIApplicationSceneManifest'; fi
delegate=$(plutil -extract UIApplicationSceneManifest.UISceneConfigurations.UIWindowSceneSessionRoleApplication.0.UISceneDelegateClassName raw "$plist" 2>/dev/null || true)
if [ -n "$delegate" ]; then ok "the application role's scene delegate is $delegate"; else bad 'no scene delegate for UIWindowSceneSessionRoleApplication'; fi
# A Swift class named Module.Class in Info.plist is registered with the Objective-C runtime as _TtC<len>Module<len>Class.
objc=$(echo "$delegate" | awk -F. 'NF==2{printf "_TtC%d%s%d%s", length($1), $1, length($2), $2}')
if [ -n "$objc" ] && strings -a "$bin" | grep -qxF "$objc"; then ok "the binary registers $delegate"; elif [ -n "$delegate" ]; then bad "the binary has no class named $delegate"; fi
multi=$(plutil -extract UIApplicationSceneManifest.UIApplicationSupportsMultipleScenes raw "$plist" 2>/dev/null || true)
if [ "$multi" = false ]; then ok 'one window: UIApplicationSupportsMultipleScenes is false in the manifest'; else bad "UIApplicationSupportsMultipleScenes in the manifest is '$multi', wanted false"; fi
exit $fail
