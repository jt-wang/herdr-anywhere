# Herdr Anywhere

Your agents in Herdr, on your phone.

<p>
  <img src="https://herdr-anywhere.jingtao.io/type.jpg" width="32%" alt="Pi's terminal on a phone, working on a sentence typed there, with the terminal keys below">
  <img src="https://herdr-anywhere.jingtao.io/agents.jpg" width="32%" alt="The phone's home screen: Claude Code, Kimi and Pi across four spaces">
  <img src="https://herdr-anywhere.jingtao.io/needs.jpg" width="32%" alt="Two agents waiting for an answer: Kimi asks to trust a folder, Claude Code asks to run npm test">
</p>

You leave your computer with three agents running. One finishes, one stops to ask before it runs a command, and you find out an hour later. With Herdr Anywhere your phone plays Herdr's "needs you" sound the moment an agent stops. You open its terminal, answer, and it carries on. From the same screen you can start another agent in any folder, on Wi-Fi or on 4G.

The connection is encrypted end to end. There is no account to create and no server of your own to run. Fully open source, under the MIT license. [The website](https://herdr-anywhere.jingtao.io/) shows it running.

## What it does

- **Alerts, as on the desktop.** An agent that becomes blocked or fails always alerts; one that finishes alerts if you weren't looking at it. Each alert plays Herdr's own sound and shows a toast. Settings › Alerts turns each kind off.
- **Live terminals.** Each pane streams from herdr-core as it does to the desktop app. Swiping scrolls and keeps gliding, in full-screen programs like Pi too. A key bar above the keyboard has what a phone keyboard lacks: Esc, Tab, Ctrl, the arrows, paste and copy.
- **Start work.** Open a new tab or space with a shell or any agent Herdr knows, and split panes side by side.
- **Stays connected on Android.** The app keeps its connection in the background, with a quiet notification saying so, and an agent's question reaches you as a notification. Settings can turn this off.
- **Updates itself.** Home says when a new version is out, and one tap installs it on the computer, then on the phone. The app asks first whether the computer may check: after a yes it asks GitHub once a day, and Settings can turn that off, check right away, or have the computer install new versions by itself. The phone reaches GitHub only to download the app after you tap Download.
- **Your phone's language.** English, Simplified and Traditional Chinese, Spanish, Korean, Japanese and Russian, following the phone.

## Try it

1. Paste this into your coding agent on the computer running [Herdr](https://herdr.dev). It follows [INSTALL.md](INSTALL.md) and checks each step:

   ```text
   Read https://raw.githubusercontent.com/jt-wang/herdr-anywhere/main/INSTALL.md and install Herdr Anywhere on this computer.
   ```

   <details>
   <summary>Or install it yourself</summary>

   ```sh
   mkdir -p ~/.local/bin
   curl -fsSL -o ~/.local/bin/herdr-anywhere https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-macos-arm64
   chmod +x ~/.local/bin/herdr-anywhere
   ~/.local/bin/herdr-anywhere install-dialog
   ```

   On Windows, in PowerShell:

   ```powershell
   New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\herdr-anywhere" | Out-Null
   curl.exe -fsSL -o "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe" https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-windows-amd64.exe
   & "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe" install-dialog
   ```

   </details>

2. On the phone, download [Herdr-Anywhere.apk](https://github.com/jt-wang/herdr-anywhere/releases/latest/download/Herdr-Anywhere.apk) and open it.

3. In the Herdr desktop app press `cmd-shift-m` (`ctrl-shift-m` on Windows), press Start, and scan the QR code with the phone app.

The phone and the computer can be on different networks from then on. Paired phones are listed under the code, and you can remove them there. After a restart of the computer, press Start in the dialog once.

### From a terminal instead of the dialog

```sh
~/.local/bin/herdr-anywhere serve --tailcat     # keep it running
~/.local/bin/herdr-anywhere pair                # in another terminal: shows a QR code that works once, for 2 minutes
```

`herdr-anywhere devices` lists the paired phones and `herdr-anywhere revoke <id>` cuts one off at once.

### Over your own network only

This uses no relay. It is also not encrypted, so use it on a network you trust. The phone and the computer must be on the same network.

```sh
~/.local/bin/herdr-anywhere serve --lan         # keep it running
~/.local/bin/herdr-anywhere pair                # prints a link like http://192.168.1.20:7681/#pair=…
```

Paste the link into the app. It works once, for 10 minutes.

### In a browser

The companion can serve the web build (see below) over your network. Start it like this and open the link `pair` prints. The page posts a system notification when an agent needs you while it's in the background; Settings › Alerts asks the browser for permission.

```sh
herdr-anywhere serve --lan --web "app/dist/web/Herdr Anywhere"
```

## iPhone and other agents

**iPhone:** there is no ready-made app yet. You can build it under your own Apple account with [app/ios/README.md](app/ios/README.md). While it's open it plays the sound and shows the toast. It posts no system notifications.

**Agents:** anything that runs in Herdr opens in a terminal you can type into. Status and alerts come from each agent's own reports. Claude Code, Pi, Kimi and Antigravity send them. OpenCode, Grok, Cursor and Codex 0.159 don't, so they work without alerts.

## Security

herdr-core can run any command as you, and so can a paired phone. The companion:

- listens on `127.0.0.1` unless you pass `--lan`, which adds the computer's private LAN address (never `0.0.0.0`);
- accepts only paired phones; a pairing code works once and expires after 10 minutes;
- closes a revoked phone's connections at once.

Through [Tailcat](https://github.com/tailscale/tailcat), which the dialog and `serve --tailcat` use, traffic is end-to-end encrypted with WireGuard. It goes straight to the computer when it can, otherwise through Tailcat's public relays, which see only encrypted packets. Settings › Computer shows which one it is using.

Over `--lan`, traffic is plain `ws://`, which anyone on that network can read.

Updates install only what the release's signed list says: the computer checks the list against a key built into it, then each file's SHA-256 and size, and Android also requires the app to carry the same signing key as the installed one.

## Build from source

The app is TSX on [NSH](https://nsh.dev) 0.3.0, in `app/`. The companion is Go, in `companion/`.

```sh
git clone https://github.com/jt-wang/herdr-anywhere
cd herdr-anywhere/companion
go build -o ~/.local/bin/herdr-anywhere ./cmd/herdr-anywhere    # needs Go 1.27.1 or newer
~/.local/bin/herdr-anywhere install-dialog
```

| | Command | Output |
|---|---|---|
| Android | `app/android/build.sh` (needs a JDK, the Android SDK and NDK, and Go) | `app/android/build/Herdr.apk` |
| iOS simulator | `app/ios/build.sh App.tsx simulator` (needs Xcode, xcodegen and Go) | `app/ios/build/Herdr.app` |
| iPhone, under your Apple account (not yet tried on a phone) | [app/ios/README.md](app/ios/README.md) | `app/ios/build/Herdr.app` |
| Web | `cd app && nsh package --target web -o dist/web` | `app/dist/web/Herdr Anywhere` |
| The release's companion | `cd companion && CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags '-s -w' -o herdr-anywhere-macos-arm64 ./cmd/herdr-anywhere` | `companion/herdr-anywhere-macos-arm64` |

Tests:

```sh
cd companion && go test ./...
cd app && scripts/test.sh                                # unit and screen tests
ANDROID_SERIAL=emulator-5554 app/e2e/android.sh          # on a device or emulator
ANDROID_SERIAL=emulator-5554 app/e2e/android-switcher.sh <apk> <companion>   # the switcher's buttons and swiping, with real taps
ANDROID_SERIAL=emulator-5554 app/e2e/android-update.sh <files listed in the script>   # updating the computer and the app
SIM_UDID=<simulator udid> app/e2e/ios.sh                 # on a simulator
SIM_UDID=<simulator udid> app/e2e/ios-tailcat.sh         # on a simulator, through TailPocket (needs the internet)
SIM_UDID=<simulator udid> app/e2e/ios-glyphs.sh          # on an iOS 26 simulator: agents' symbols are not the missing box
SIM_UDID=<simulator udid> app/e2e/ios-keyboard.sh        # on a simulator: the keyboard comes up for the terminal, with real taps
app/e2e/user-path.sh <folder with the release files>     # INSTALL.md's steps, without Go
```

Every pull request runs the companion, unit, screen and page tests and a secret scan. [CONTRIBUTING.md](CONTRIBUTING.md) says
which test to write for which change.

## License

Herdr Anywhere's own code is [MIT](LICENSE). It uses Herdr's icons, program table and sounds from [herdr-app](https://github.com/herdrdev/herdr-app)
(Apache License 2.0). [NOTICE](NOTICE) lists everything it includes and the licenses.
