# Herdr Anywhere on your iPhone

Build the app under your own Apple account and install it on your phone. To have your coding agent do it, paste this:

> Read app/ios/README.md in https://github.com/jt-wang/herdr-anywhere and build the app for my iPhone. Ask me for what only I can do.

On a free Apple account, Apple asks you to rebuild and reinstall every [7 days](https://developer.apple.com/support/compare-memberships/). The build has not been run on a real iPhone yet, so please [open an issue](https://github.com/jt-wang/herdr-anywhere/issues) if a step fails.

## What you need

- A Mac with Apple silicon and Xcode, with your Apple account added in Xcode → Settings → Apple Accounts.
- These tools:

  ```sh
  brew install go xcodegen gh
  curl -fsSL https://nsh.dev/install.sh | NSH_VERSION=v0.3.0 sh
  ```

- Your iPhone connected by cable and trusted, with Settings → Privacy & Security → Developer Mode on.

## Build and install

1. Clone the app and get its patched NativeShell framework. Choose a successful [Devices run](https://github.com/jt-wang/herdr-anywhere/actions/workflows/devices.yml) for the revision you are building, with the `nsh-ios-xcode27` artifact. Use the same Xcode version as that run. If no artifact is available, run Devices on your fork with `groups=ios27-chat`.

   ```sh
   git clone https://github.com/jt-wang/herdr-anywhere
   cd herdr-anywhere
   export NSH_IOS_EMBED=/path/on/your/build-volume/nsh-ios
   mkdir -p "$NSH_IOS_EMBED"
   gh run download <run-id> -R jt-wang/herdr-anywhere \
     -n nsh-ios-xcode27 -D "$NSH_IOS_EMBED"
   (cd "$NSH_IOS_EMBED" && shasum -a 256 -c NativeShell-ios.tar.gz.sha256 && \
     tar -xzf NativeShell-ios.tar.gz)
   test "$(cat "$NSH_IOS_EMBED/nsh-build.txt")" = "$(app/ios/nsh-patches/key.sh)"
   ```

   For a fork's run, replace `jt-wang/herdr-anywhere` with your fork. The archive contains both the iPhone and Apple silicon simulator frameworks. Keep `NSH_IOS_EMBED` set for the build commands below; a mismatched patch version stops the build.

2. Say who signs it. `DEVELOPMENT_TEAM` is your Team ID. `BUNDLE_ID_PREFIX` is anything of your own, in the form `com.yourname`.

   ```sh
   cat > app/ios/Signing.local.xcconfig <<'EOF'
   DEVELOPMENT_TEAM = ABCDE12345
   BUNDLE_ID_PREFIX = com.yourname
   EOF
   ```

   On a paid account, your Team ID is at [Membership details](https://developer.apple.com/account#MembershipDetailsCard). On any account, make a signing certificate in Xcode → Settings → Apple Accounts → Manage Certificates (+ → Apple Development), then this prints the ID as `OU`:

   ```sh
   security find-certificate -c "Apple Development" -p | openssl x509 -noout -subject
   ```

3. Build it for your phone, whose id `xcrun devicectl list devices` shows. This passes when it prints `→ ios/build/Herdr.app`.

   ```sh
   DEVICE=<id> app/ios/build.sh App.tsx device
   ```

4. Install it. If iOS won't open the app, trust your account under Settings → General → VPN & Device Management.

   ```sh
   xcrun devicectl device install app --device <id> app/ios/build/Herdr.app
   ```

For the simulator, prepare the framework in step 1, then run `app/ios/build.sh App.tsx simulator`; signing is only needed for a phone.

## Pair it

Set up the Mac with [INSTALL.md](../../INSTALL.md), steps 1 to 3. Then press `cmd-shift-m` in the Herdr desktop app, press Start, and tap Scan QR code in the app.

To look around before that, tap Try it. It runs on sample data and needs no computer.
