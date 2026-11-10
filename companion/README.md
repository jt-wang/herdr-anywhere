# herdr-anywhere companion

The Mac side of Herdr Anywhere. It lets a paired phone use herdr-core (`hc`) on this Mac: for each phone WebSocket it
starts one `hc connect` and copies bytes both ways unchanged. The phone speaks herdr-core's protocol itself. The
companion also serves the PWA, so the page and its WebSocket share one origin.

## Use it

```sh
go build ./cmd/herdr-anywhere          # needs Go 1.27.1
./herdr-anywhere serve --lan           # 127.0.0.1:7681 plus this Mac's private LAN address
./herdr-anywhere serve --tailcat       # also reachable through Tailcat, from any network
./herdr-anywhere pair                  # prints a QR code and a link; scan it with the phone
./herdr-anywhere devices               # id, name, created, last seen
./herdr-anywhere revoke <id>           # the device's live connections close within a second
./herdr-anywhere start                 # serve --tailcat in the background, on any free local port, and again at each login
./herdr-anywhere start --lan           # the same, also on this computer's LAN address at port 7681
./herdr-anywhere stop                  # stops it and takes it off login
./herdr-anywhere install-dialog        # adds the Add phone dialog and its key to the Herdr desktop app (start does too)
```

`pair --json` and `devices --json` are what the dialog reads: `pair --json` prints `{invite, expires, qr}`, with `qr` as
rows of 0 and 1, or `{"error":"not-running"}` and exit 1 without creating a code. `start` logs to `serve.log` (0600) in
the state dir. start writes a login entry that runs `serve --tailcat --listen 127.0.0.1:0 --state DIR` with the PATH it
was run with: a LaunchAgent (`~/Library/LaunchAgents/io.jingtao.herdranywhere.companion.plist`, RunAtLoad, KeepAlive
on failure) that launchd also runs now, a systemd user unit (`herdr-anywhere.service`, Restart=on-failure,
KillMode=process) that systemd also starts now, or on Windows the `Herdr Anywhere` value under the Run key (with
`--log`), which starts it from the next login; start runs it itself then, and wherever the manager fails. An update
replaces the file at the same path, so the entry stays right. `stop` removes the entry and ends a serve that answers on
the state dir. With no serve running, `pair` (but not `pair --json` or `pair --url`) starts one as start does.

`serve` options: `--listen ADDR` (repeatable), `--lan`, `--tailcat`, `--web DIR` (the PWA build), `--hc PATH`,
`--state DIR`.

With `serve --tailcat` running, `pair` asks it for a Tailcat invite (`herdr1:…`, valid 2 minutes) and prints that as
the QR code for the Herdr Anywhere app, plus the `#pair=` link for the same single-use code when serve listens on the LAN.
The Tailcat identity (private keys) and paired phones' keys are in `tailcat.json` (0600) in the state dir; the
control socket `serve.sock` (0600) is there too, so the state dir path must stay under 103 bytes. `revoke` also drops
the phone from Tailcat, which restarts Tailcat's node: every phone's Tailcat connections drop and reconnect.
`pair` options: `--name`, `--ttl` (default 10m), `--url` (default: this Mac's LAN IPv4 on the port the running `serve`
listens on, else 7681; pass your tailnet name here to pair over the tailnet). `pair` works whether or not `serve` is running; a running `serve` accepts the new code
at once.

`hc` is found as `--hc`, else `hc` on PATH, else `~/.local/bin/hc`; it must print `hc <version>` for `--version`.
`hc connect` inherits the companion's environment, so `HC_SESSION` / `HC_SOCKET` choose the core.

## HTTP

| Route | What |
|---|---|
| `GET /api/info` | `{"name","version","hc","latest","canUpdate","autoUpdate","checks","app"}`; no auth, no secrets. `checks` is `"ask"` until a phone answers whether the computer may ask GitHub about new releases, then `"on"` (once a day) or `"off"`. `latest` is the newest release the last check found, `""` before any. `app` is `{"url","digest","bytes"}` for the Android app in that release |
| `POST /api/pair` | `{"code","device"}` → `{"token","device","name"}`; 401 unknown code, 410 expired or used, 429 after 10 failures a minute from one IP |
| `GET /api/folders?path=` | A folder's subfolders, for picking a space's folder: `{"path","home","parent","git","folders":[{"name","git"}]}`, hidden ones left out unless `hidden=1` (then marked `"hidden":true`); `~` and empty mean home. 404 `{"missing":true,"path","existing"}` when it does not exist (`existing` is the nearest folder above that does), 400 for a file or a relative path. `Authorization: Bearer <token>` |
| `POST /api/folders` | `{"path"}` → `{"path","created"}`: makes the folder and its missing parents, after the phone asked; 409 when part of the path is a file. Same auth |
| `POST /api/update` | Paired phones. Installs the newest release, answers `{"version"}`, then runs it as the same process. 409 when there is nothing newer or an update is under way, 501 where no released file runs, 502 when the download fails or doesn't match the release's digest, which leaves the old file in place |
| `POST /api/update/auto` | Paired phones. `{"on":true}` installs new releases without asking, and turns daily checks on; off until set, kept in the state dir |
| `POST /api/update/checks` | Paired phones. `{"on":true}` lets the computer ask GitHub about new releases once a day; `{"on":false}` stops it and turns off installing by itself. Answers `{"checks"}` |
| `POST /api/update/check` | Paired phones. Asks GitHub once, now, and answers `{"latest"}`; installs nothing. 502 when GitHub can't be reached |
| `POST /api/files?name=[&on=machine]` | A photo or file from the phone, the raw bytes as the body, up to 2 GB: `{"path","bytes"}`, its path on that computer for the agent to read. It lands in `~/.cache/herdr-anywhere/uploads`, kept 7 days. 413 when too large, 502 when the machine can't be reached |
| `GET /hc?token=…[&enc=b64]` | WebSocket to `hc connect`: binary messages both ways, or base64 text with `enc=b64`. 401 bad token, 403 foreign `Origin`, 503 no hc. Closes with 4401 on revoke, "core closed" when hc exits |
| anything else | files from `--web` |

## Security

herdr-core's API can run any command as you, so whoever holds a device token can too.

- `serve` binds to `127.0.0.1` by default; `--lan` adds only private RFC 1918 addresses, never `0.0.0.0`.
- Pairing codes are single use, 128-bit, and expire after 10 minutes. Device tokens are 256-bit. Only their SHA-256
  hashes are stored, in `~/.config/herdr-anywhere/state.json` (dir 0700, file 0600; `--state` or `HERDR_ANYWHERE_STATE`
  move it).
- A WebSocket with an `Origin` header must come from the companion's own origin. Request logs never include the query
  string, where the token travels.
- Plain `ws://` over the LAN can be read by anyone on that network, token included. Use it on a network you trust, or
  reach the Mac over an encrypted path such as your tailnet.
- A release counts only if its `latest.json` is signed by the Ed25519 key built into the companion
  (`internal/update/manifest.go`). It lists each file's SHA-256 and size; the companion installs a file only if it
  matches, runs, and says it is that version, and tells phones the app's SHA-256 from the same list. Releases are signed
  with `go run ./cmd/release-manifest sign -key <key> -version X.Y.Z -out <dir> <files>`, which refuses a release that
  leaves out any computer's file; the private key stays off GitHub.

## Tests

`go test ./...` runs unit tests and, when `~/.local/bin/hc` exists, integration tests against a real herdr-core in its
own scratch session (`HC_SESSION=hmtest-<random>`, scratch XDG dirs, stopped with `hc server stop` afterwards).
`HM_TAILCAT_LIVE=1 go test ./cmd/herdr-anywhere -run TestLiveTailcat -v` also pairs an in-process Go phone through
Tailcat's real relays and opens `/hc` through its forwarder; it needs the internet and `hc`.

Dependencies: `github.com/coder/websocket` (ISC), `github.com/mdp/qrterminal/v3` (MIT) and its `rsc.io/qr` (BSD-3-Clause),
TailPocket (`github.com/jt-wang/tailpocket`, MIT: its packages are copied into
the repository's `third_party/tailpocket-8ae23dd`; it brings in `tailscale.com` and `github.com/tailscale/tailcat`).
