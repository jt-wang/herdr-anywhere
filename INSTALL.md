# Installing Herdr Anywhere (for AI agents)

Run these steps in order on the user's computer: the shell commands on a Mac or Linux, the PowerShell ones on Windows. They
only download files from the latest release; nothing is cloned or built. Each step ends with a check; stop and tell the
user what failed if a check does not pass.

## 1. herdr-core is installed

```sh
herdr-core --version || ~/.local/bin/herdr-core --version
```

```powershell
hc --version; if (-not $?) { herdr-core --version }
```

Passes when it prints `hc <version>`. If neither command exists, ask the user to install Herdr
(https://github.com/herdrdev/herdr-app, whose installer also installs herdr-core). Do not run that installer without
the user's go. On an Intel Mac, Herdr's installer stops: herdr-core has its own installer there
(`curl -fsSL https://releases.herdr.dev/core/install.sh | sh`), and the Herdr desktop app has no Intel build. Without
the app, skip step 3 and pair from a terminal: the README's "From a terminal instead of the dialog".

## 2. Download the companion

On a Mac, the file for the processor `uname -m` names, `arm64` (Apple silicon) or `x86_64` (Intel):

```sh
uname -m
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/herdr-anywhere https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-macos-$(uname -m | sed 's/x86_64/amd64/')
chmod +x ~/.local/bin/herdr-anywhere
~/.local/bin/herdr-anywhere --version
```

Passes when the last command prints `herdr-anywhere <version>`. If `~/.local/bin` is not on the user's PATH, call it by
its full path below.

On Linux, the file for the machine `uname -m` names, `x86_64` or `aarch64`:

```sh
uname -m
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/herdr-anywhere https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-linux-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
chmod +x ~/.local/bin/herdr-anywhere
~/.local/bin/herdr-anywhere --version
```

Passes when the last command prints `herdr-anywhere <version>`. For another machine the download fails: there is no
ready-made companion for it, and the user has to build one.

On Windows it goes next to Herdr's own programs:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\herdr-anywhere" | Out-Null
curl.exe -fsSL -o "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe" https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-windows-amd64.exe
& "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe" --version
```

Passes when it prints `herdr-anywhere <version>`. Below, call it by that full path, `& "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe"`, wherever
the steps say `~/.local/bin/herdr-anywhere`. It updates itself in that folder later, so leave it there.

## 3. Add the pairing dialog to the Herdr desktop app

Tell the user first: the dialog is a Herdr mod that runs `herdr-anywhere` on this computer, and this step adds one key to their
`keys.json`.

```sh
~/.local/bin/herdr-anywhere install-dialog
```

```powershell
& "$env:LOCALAPPDATA\Programs\herdr-anywhere\herdr-anywhere.exe" install-dialog
```

Passes when it prints `Add phone is installed`. It says which key opens the dialog (`cmd-shift-m` on a Mac,
`ctrl-shift-m` on Windows and Linux, or the key to bind yourself if that one is taken). If it says to open Herdr once, do that and run it again.
`herdr-anywhere start` also adds the dialog when it isn't there, and `start`, the companion's background service and
`herdr-anywhere update` bring it up to date with each new version; this step is here so the user hears about
`keys.json` first. Run it again after moving the `herdr-anywhere` binary: the dialog keeps its full path.

## 4. Pair the phone

Tell the user to install the app on their Android phone from https://github.com/jt-wang/herdr-anywhere/releases/latest/download/Herdr-Anywhere.apk, press the
dialog's key in Herdr, press Start, and scan the QR code with the app. An iPhone has no ready-made app yet: build it for the
user with [app/ios/README.md](app/ios/README.md), which needs their Apple account and their phone on a cable.

Check:

```sh
~/.local/bin/herdr-anywhere devices
```

Passes when the phone is listed. `herdr-anywhere revoke <id>` removes it.

## If the user doesn't use the Herdr desktop app

Start the companion, then show the user the QR code `pair` prints. It works once, for 2 minutes.

```sh
herdr-anywhere start
herdr-anywhere pair
```

`start` passes when it prints `started` or `already running`. It keeps the companion running in the background and
starts it again when the user logs in after a restart (a LaunchAgent on macOS, a systemd user unit on Linux, the Run
key on Windows). `herdr-anywhere stop` undoes both.

## If the user wants their own network only

This is not encrypted: anyone on that network can read it. Tell the user before choosing it.

```sh
herdr-anywhere start --lan
curl -fsS http://127.0.0.1:7681/api/info    # passes when it prints JSON
herdr-anywhere pair
```

`start --lan` keeps the companion running in the background on port 7681 of the LAN address, also after the computer
restarts, so the terminal can be closed. `herdr-anywhere stop` stops it. Give the user the printed link
(`http://<LAN address>:7681/#pair=…`) to paste into the app. It works once and expires after 10 minutes. The companion
also runs Tailcat: `pair` opens a Tailcat invite as well, which contacts a Tailcat relay, though a phone that pairs
with the link never uses it. If the user wants no relay contacted at all, run `herdr-anywhere serve --lan` in place
of `start --lan`; it stops when its terminal closes. If it says `--lan: this computer has no private IPv4 LAN address`, the computer is not on a Wi-Fi or Ethernet
network the phone could share.

## If the user's agents run on a server they reach with ssh

There are two ways. Ask the user which one fits:

- **The computer reaches the server.** herdr-core on the user's computer logs in to the server with ssh, and the phone
  sees the server through the companion on the computer. Nothing is installed on the server but herdr-core. The
  computer has to stay on.
- **The companion runs on the server.** The phone pairs with the server itself, and the computer can be off. This
  needs `sudo` on a Linux server.

### The computer reaches the server

Do steps 1 to 4 on the user's computer first. Then add the server to herdr-core there. `my-server` stands for how ssh
reaches it: a `Host` in `~/.ssh/config`, `user@host` or `user@host:port`. Check first that `ssh my-server true` logs in
without asking for anything.

```sh
herdr-core machine add my-server --install
herdr-core machine ls
herdr-core exec --on my-server hostname
```

Passes when `machine ls` shows `my-server` as `connected` (run it again if it still says `connecting`) and the last
command prints the server's host name.
`--install` copies herdr-core to the server when it has none or an older one. If the server asks for a password or a
code, run `herdr-core machine login my-server` in a terminal the user can type into.

On the phone, home lists the server's spaces under the server's name, and the `+` on one of them starts an agent on
the server. A space started from the phone's own `+` runs on the computer. When the server is unreachable,
`herdr-core machine ls` shows it as `connecting` until it comes back.

### The companion runs on the server

Run these on the server, logged in as the user whose agents run there. First herdr-core, if `herdr-core --version`
finds none:

```sh
curl -fsSL https://releases.herdr.dev/core/install.sh | sh
~/.local/bin/herdr-core --version
```

Passes when it prints `hc <version>`. Then download the companion with the Linux commands of step 2. Then keep it
running with systemd, so it starts again after a reboot or a crash:

```sh
sudo tee /etc/systemd/system/herdr-anywhere.service >/dev/null <<UNIT
[Unit]
Description=Herdr Anywhere companion
Wants=network-online.target
After=network-online.target

[Service]
User=$USER
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=$HOME/.local/bin/herdr-anywhere serve --tailcat
Restart=always
RestartSec=5
KillMode=process

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable --now herdr-anywhere
systemctl is-active herdr-anywhere
```

Passes when the last command prints `active`. `KillMode=process` keeps the agents' terminals running when the
service restarts. Then show the user the QR code this prints in the terminal, and have them scan it
with the app:

```sh
~/.local/bin/herdr-anywhere pair
~/.local/bin/herdr-anywhere devices
```

The code works once, for 2 minutes. Run `devices` after the user scans: it passes when the phone is listed. Home on the
phone then shows the server's spaces, and new spaces start on the server. `sudo journalctl -u herdr-anywhere` has the
companion's log.
