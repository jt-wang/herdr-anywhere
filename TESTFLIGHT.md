# Build Herdr for TestFlight

A cloud coding agent can request an iOS build through GitHub Actions. Apple signing credentials stay in the repository's `testflight` environment; the agent needs a GitHub token with **Actions: write** and **Contents: read**.

## 0.3.8 (43)

Update the Companion too (it updates itself, or `herdr-anywhere start` after installing 0.3.8). Stop the Companion on the computer (`herdr-anywhere stop`) with the app open: after about 18 s the banner says Still can't reach your computer, with Try again and Other options, and the status bar says Not connected · retrying. Other options lists the four causes with what to do; Copy copies `herdr-anywhere start`. Start the Companion again: the app connects by itself with the same pairing. Pair again from that list opens pairing with Back, and Back returns to the current pairing.

## 0.3.7 (42)

The Companion is the same as 0.3.6, and the steps below still apply. With Tailcat only picked against `--routes lan`, the banner says in one sentence that the computer hasn't turned that way on, and Use Automatic connects again; home, Settings and the terminal all say Not connected until then. On Android, open Settings, then a terminal's input box, then Settings again: every description shows whole (see CHANGELOG.md).

## 0.3.6 (41): TailPocket v0.3.3, Tailcat optional

Built on TailPocket v0.3.3 (jt-wang/tailpocket@32c50f3aac847044030222fe9b498bb98e882133). Tailcat is now one route of three. Use the phone and Companion from the same build.

1. **Local network only.** On the computer, `herdr-anywhere start --routes lan` (or `serve --routes lan --listen 127.0.0.1:0`). Pair the phone on the same Wi-Fi: the emoji check works as before, and the invite lists only the local network. Settings › Connection shows Automatic; the Status line says Connected and the path says the local network. Leave the Wi-Fi: the phone doesn't connect, which is what that choice costs.
2. **Settings › Connection.** Pick each of Automatic, Local network only, Without Tailcat and Tailcat only. Each pick closes the open tunnel and the connection comes back at once on a route it allows (Status goes Reconnecting, then Connected). A pick the computer doesn't run (Tailcat only against `--routes lan`) does not connect: the banner says the phone and the computer have no route in common, and its Connection button opens the list.
3. **Relay only.** `herdr-anywhere start --routes relay --relay wss://… --relay-token-file FILE`, Without Tailcat on the phone, with the phone off the computer's network: Settings shows the relay.
4. **Clear errors.** `herdr-anywhere serve --lan` (without `--tailcat`), `start --routes lan,relay` without `--relay`, `start --routes lan --tailcat-only` and `serve --tailcat --routes lan` each stop at once with a message that names the fix.
5. A phone that had Settings › Tailcat only on before this build opens with Connection set to Tailcat only.

## 0.3.5 (40)

The Companion is the same as 0.3.4, and the steps below still apply. Settings › Language picks the app's language (Follow system, or one of the seven) and changes it at once; on iOS it is the same setting as Settings › Herdr Anywhere › Language (see CHANGELOG.md).

## 0.3.4 (39)

The Companion is the same as 0.3.3, and the steps below still apply. The phone app no longer reconnects every few seconds on Wi-Fi networks where iOS keeps reporting network changes: it checks the open connection first and rebuilds it only when no answer comes (see CHANGELOG.md).

## 0.3.3 (38)

The phone app is the same as 0.3.2 (37), and the steps below still apply. The Companion that starts at login no longer restarts every 10 seconds when another Companion is already running (see CHANGELOG.md).

## 0.3.2 (37) release candidate

Built on TailPocket v0.3.1 (jt-wang/tailpocket@086e368148aac78dc7ad21eb18b3a98d69a0590a), with immediate revocation on every route and bounded shutdown. Formal release requires real blocked-UDP and Android diagnostic acceptance. This candidate also preserves chat reading positions, revalidates unchanged chat with ETags, retries recoverable terminal attachments in place, and provides opt-in diagnostics on iOS and Android in Settings › On-device diagnostics. Use the phone and Companion from the same release; check `herdr-anywhere --version` says `0.3.2`. Build 37 is reserved in source, not an upload receipt.

**Update the phone and the computer together.** A phone on 0.3.0 (35) or older cannot pair with a 0.3.1 or 0.3.2 Companion: `pair` shows only `herdr2:` invites, which need the emoji check. Phones already paired keep connecting. A 0.3.1 (36) phone pairs with a 0.3.2 Companion, and a 0.3.2 phone with a 0.3.1 Companion. Only a 0.3.2 Companion sends a refusal at once, so report the timing in step 2 only with 0.3.2 on both sides. Mixed TailPocket test builds (pre.1–pre.5) are not supported.

On device:

1. **Upgrade without a new QR code.** Pair a phone on 0.2.1 (App Store), 0.3.0 (35) or 0.3.1 (36) with that version's Companion. Update the Companion to 0.3.2 and the phone to 0.3.2 (37), and open the app: it connects without scanning again. A phone paired before 0.3 registers its end-to-end key on that first connect: in the Companion's `tailcat.json` its key gets a `keys` entry, and the serve log no longer counts it among phones "paired before 0.3". Settings then shows lan or relay when those are on. Revoke it afterwards, so later steps start clean.
2. **Emoji pairing.** Run `herdr-anywhere pair` and scan its QR code. The phone shows four emoji with "Check that your computer shows these same four emoji, then confirm there". `pair` shows the phone's name and four emoji and asks `[y/N]`; they must be the same four. `y` pairs. Then test each way of not pairing, with a new `pair` each time:
   - **Answer no on the computer** (Enter at `[y/N]`, or Don't pair in the dialog): the phone says at once, within a second or two, that the computer did not confirm.
   - **Close the app on the phone** while `pair` asks.
   - **Wait over 55 s** without answering.

   Each time, `pair` says the code no longer works, scanning that code again fails, and a new `pair` works. Also try the Herdr desktop Add phone dialog (cmd-shift-m): it shows the emoji with Pair and Don't pair, and Enter does not answer.
3. **Local network only.** Run `herdr-anywhere start` (the local network is on by default). Put the phone on the same Wi-Fi with cellular off: Settings shows "Over your local network, end-to-end encrypted". Switch Wi-Fi off: it moves to Tailcat (direct or relay) without pairing again.
4. **Relay only.** Test the two relay types separately. For a configured tailpocket-relay, use `herdr-anywhere serve --tailcat --no-lan --relay wss://… --relay-token-file FILE` (`start` has no relay option), with Tailcat only off on the phone and LAN unavailable to both sides, and confirm Settings measures relay. For Tailcat's DERP-only path, use `herdr-anywhere start --tailcat-only` with no configured relay and enable Tailcat only on the phone. Apply a real IPv4 and IPv6 firewall block of all non-DNS UDP on both sides (the iOS simulator shares its Mac's network; a real phone needs a router/network block). On Linux, TailPocket v0.3.1 provides `scripts/e2e-udp-block.sh on|off`: run it in an isolated test network namespace, since it affects every process in that namespace, including loopback. Verify a STUN query to `stun.l.google.com:19302` times out while HTTPS still works, then confirm Settings measures DERP. Exercise refused pairing, revoke the last phone while its connection is open, reconnect, large uploads (messages are at most 64 KiB each), and SIGTERM exit within seconds; always run `off` in a cleanup trap. Skip same-Wi-Fi pairing while UDP is blocked because mDNS needs UDP. After removing the block, restore LAN and verify new connections move to LAN without cutting an already-open connection.
5. **Tailcat only, on the computer.** Run `herdr-anywhere stop`, then `herdr-anywhere start --tailcat-only`.
   - The serve log says "Tailcat only".
   - Nothing listens on the local network: `lsof -iTCP -sTCP:LISTEN -P | grep herdr` shows no private address.
   - A new `pair` QR code carries only Tailcat.
   - Phones still connect, also after a restart (the login entry keeps the flag).
   - `start --tailcat-only --lan` is refused.
6. **Tailcat only, on the phone.** Keep the computer on the default `start` and turn on Settings › Computer › Tailcat only.
   - The connection drops once and comes back.
   - Settings shows Direct to your computer or Through a relay (Tailcat's), never the local network, also on the same Wi-Fi.
   - The setting stays on after the app restarts.
   - Pair a new phone with it on: it pairs over Tailcat.
7. **Revoke.** Run `herdr-anywhere devices`, then `herdr-anywhere revoke <id>`, or use Remove in the Add phone dialog.
   - That phone's connections close within a second on every route, and the app asks to pair again.
   - Other phones keep working; their Tailcat connections may drop and reconnect once.
8. **Same-Wi-Fi 6-digit pairing** (TailPocket's ServeLocal, where a window ends with "too many attempts, start again" after 3 abandoned or refused attempts): Herdr Anywhere does not offer this pairing yet, so there is nothing to test in this build.

The plain LAN port of 0.2 still behaves as in 0.3.0:
- it opens only with `--lan`;
- it stays open only while a phone paired by a `#pair=` link is left, and for at most 3 days after the first 0.3 serve;
- it never serves a phone that has an end-to-end key.

Two availability checks, as for 0.3.0:

- **Slow uplink over relay.** On a cellular network with a limited uplink, upload several tens of MB in a row through the relay. The session must not be declared silent and dropped after about 5 seconds (the phone's stall check, 2 × 3 s) while the upload still moves.
- **Many streams, slow reader.** Open 4 or more terminals with the computer printing a lot in each (`yes`, a long build log) while the app reads slowly (background it briefly, scroll a busy pane). The 16 MiB unread limit must not close the session; hidden terminals pause instead.

Report failures with the diagnostics export (Settings), the Companion's `serve.log`, and the route Settings showed.

## 0.3.1 (36) test candidate

Stage-3 device build on the public TailPocket v0.3.0-pre.6 (jt-wang/tailpocket@342ebe543f77322036f15aa93132e02078e9e478): end-to-end encrypted routes over Tailcat, the local network and a tailpocket-relay, now with the emoji check on pairing and a Tailcat-only mode on both sides. Internal TestFlight, Android test APK and the test Companion pre-release `testflight-0.3.1-36` only: not a public Release, Latest update or website change.

**Update the phone and the computer together.** A phone on 0.3.0 (35) or older cannot pair with the 0.3.1 Companion: `pair` now shows only `herdr2:` invites, which need the emoji check. Phones already paired keep connecting. Mixed TailPocket test builds (pre.1–pre.5) are not supported: report only with 0.3.1 (36) on both sides.

Install the test Companion from the pre-release (its notes have the commands), check `herdr-anywhere --version` says `0.3.1-testflight.36`, then on device:

1. **Upgrade without a new QR code.** Pair a phone on 0.2.1 (App Store) or 0.3.0 (35) with the computer's old Companion. Update the Companion to 0.3.1 and the phone to 0.3.1 (36), and open the app: it connects without scanning again. A phone paired before 0.3 registers its end-to-end key on that first connect (in the Companion's `tailcat.json` its key gets a `keys` entry; the serve log no longer counts it among phones "paired before 0.3"), and Settings then shows lan or relay when those are on. Revoke it afterwards, so later steps start clean.
2. **Emoji pairing.** `herdr-anywhere pair`, scan its QR code: the phone shows four emoji with "Check that your computer shows these same four emoji, then confirm there", and `pair` shows the phone's name and four emoji and asks `[y/N]`. They must be the same four. `y` pairs. Again with Enter (no), and again closing the app on the phone while `pair` asks, and again waiting over 55 s: each time the phone says the computer did not confirm, `pair` says the code no longer works, and scanning that code again fails; a new `pair` works. Also from the Herdr desktop Add phone dialog (cmd-shift-m): it shows the emoji with Pair and Don't pair; Enter does not answer.
3. **Local network only.** `herdr-anywhere start` (the local network is on by default), phone on the same Wi-Fi with cellular off: Settings shows "Over your local network, end-to-end encrypted". Switch Wi-Fi off: it moves to Tailcat (direct or relay) without pairing again.
4. **Relay only.** Test the two relay types separately. For a configured tailpocket-relay, use `herdr-anywhere serve --tailcat --no-lan --relay wss://… --relay-token-file FILE` (`start` has no relay option), with the phone on cellular, and confirm Settings measures relay. For Tailcat's DERP-only path, use `herdr-anywhere start --tailcat-only` with no configured relay and enable Tailcat only on the phone. Apply a real IPv4 and IPv6 firewall block of all non-DNS UDP on both sides (the iOS simulator shares its Mac's network; a real phone needs a router/network block). Verify a STUN query times out while HTTPS still works, then confirm Settings measures relay. Exercise refused pairing, revoke, reconnect, large uploads (messages are at most 64 KiB each), and prompt SIGTERM exit; always remove only the test firewall rules with a cleanup trap. Do not use Tailcat's force-DERP debug switch: it exercises an upstream shutdown bug rather than a real blocked-UDP network.
5. **Tailcat only, on the computer.** `herdr-anywhere stop`, then `herdr-anywhere start --tailcat-only`: the serve log says "Tailcat only", nothing listens on the local network (`lsof -iTCP -sTCP:LISTEN -P | grep herdr` shows no private address), a new `pair` QR code carries only Tailcat, and phones still connect, also after a restart (the login entry keeps the flag). `start --tailcat-only --lan` is refused.
6. **Tailcat only, on the phone.** With the computer on the default `start`, turn on Settings › Computer › Tailcat only: the connection drops once and comes back, Settings shows Direct to your computer or Through a relay (Tailcat's), never the local network, also on the same Wi-Fi; it stays on after the app restarts. Pair a new phone with it on: it pairs over Tailcat.
7. **Revoke.** `herdr-anywhere devices`, then `herdr-anywhere revoke <id>`, or Remove in the Add phone dialog: that phone's connections close within a second on every route, the app asks to pair again, and other phones keep working (their Tailcat connections may drop and reconnect once).
8. **Same-Wi-Fi 6-digit pairing** (TailPocket's ServeLocal; after 3 abandoned or refused attempts a window ends with "too many attempts, start again"): Herdr Anywhere does not offer this pairing yet, so there is nothing to test in this build.

**Known issue (TailPocket pre.6, fixed in a later TailPocket):** over Tailcat, a phone whose pairing the computer refuses (a no, 55 s without an answer, or a spent code) only shows that it was not confirmed about 70 seconds later; the computer's side ends at once. Wait for it before reporting step 2 as failing, and report it only if it takes much longer.

The plain LAN port of 0.2 still behaves as in 0.3.0: open only with `--lan`, only while a phone paired by a `#pair=` link is left, for at most 3 days after the first 0.3 serve, never for a phone with an end-to-end key.

Two availability checks, as for 0.3.0:

- **Slow uplink over relay.** On a cellular network with a limited uplink, through relay, upload several tens of MB in a row. The session must not be declared silent and dropped after about 5 seconds (the phone's stall check, 2 × 3 s) while the upload still moves.
- **Many streams, slow reader.** 4 or more terminals with the computer printing a lot in each (`yes`, a long build log) while the app reads slowly (background it briefly, scroll a busy pane). The session must not be closed by the 16 MiB unread limit; hidden terminals pause instead.

Report failures with the diagnostics export (Settings), the Companion's `serve.log`, and the route Settings showed.

## 0.3.0 (35) test candidate

First stage-3 device build on TailPocket v0.3.0-pre.3 (public jt-wang/tailpocket@81f6f8b4f2ac87b6800910bb2fe8087daf4d8ab6): phones reach the computer end to end encrypted (Noise) over Tailcat, the local network (port 7682) and a tailpocket-relay. It includes everything in 0.2.2 (34). This is an Internal TestFlight and Android test APK only, not a public Release, Latest update or website change. The phone and the computer must both run 0.3.0: build the test Companion from the same commit (`cd companion && go build ./cmd/herdr-anywhere`).

Check on device:

- **Upgrade without a new QR code.** A phone paired over Tailcat with 0.2.x, upgraded to this build while the computer runs the 0.3.0 Companion, connects without scanning again; it registers its end-to-end key by itself on first connect. Settings then shows its route.
- **Plain LAN port closes.** A phone paired before 0.3 by a `#pair=` link on the LAN is served on the old plain port 7681 only while such a phone is paired and for at most 3 days after the first 0.3 serve. Home shows until when, with Pair again. After the deadline the port stays closed and the phone must pair again with a QR code. A phone that has an end-to-end key is never served on the plain port. The browser page no longer pairs: it tells the person to scan the QR code from `herdr-anywhere pair` with the iOS or Android app.
- **pre.1/pre.2 do not interoperate.** Protocol 2 (ChaCha20-Poly1305 with rekeying) differs from TailPocket pre.1 and pre.2: a phone or computer from an earlier 0.3 test build cannot talk to this one on lan or relay. Update both sides; do not mix builds when reporting failures.
- **Local network.** `herdr-anywhere start --lan` (or `serve --tailcat --lan`), then `herdr-anywhere pair`: the QR code is a `herdr2:` invite. On the same Wi-Fi, Settings should show the lan route; leaving the network falls back to Tailcat or relay without pairing again.
- **Relay.** `herdr-anywhere serve --tailcat --relay wss://… --relay-token-file FILE` (`start` has no relay option), then pair. With the phone on cellular, Settings shows relay until a direct path is found. Messages are at most 64 KiB; large uploads must still complete.
- **Revoke.** `herdr-anywhere devices`, then `herdr-anywhere revoke <id>`: that phone's connections close within a second on every route, it cannot reconnect over lan or relay, and other phones keep working (Tailcat connections may drop and reconnect once).

Two availability checks:

- **Slow uplink over relay.** On a cellular network with a limited uplink, connected through relay, upload several tens of MB in a row (attach large files). The session must not be declared silent and dropped after about 5 seconds (the phone's stall check, 2 × 3 s) while the upload is still moving. Note the network, the size and when it failed if it does.
- **Many streams, slow reader.** Open 4 or more terminals at once while the computer keeps printing large amounts of output (for example `yes` or a long build log in each) and the app reads slowly (background it briefly, or scroll a busy pane). The session must not be closed by the 16 MiB unread limit (flow-control error); hidden terminals pause instead.

Report anything that fails with the diagnostics export (Settings), the Companion's `serve.log`, and which route Settings showed.

## 0.2.2 (34) test candidate

This candidate combines paired home/recent-chat persistence, pending-text recovery, reduced background polling and hidden-terminal backpressure with opt-in local diagnostics. Diagnostics are off by default; exports use the name **Herder Anywhere Diagnostics** and require preview before the system share sheet. Nothing is uploaded automatically.

Build 34 includes build 33 and two chat fixes. Earlier-message buttons require a confirmed nonempty older page; cached text never restores process-bound paging cursors. Check new, short, exactly one-page and longer conversations, including an offline restart followed by reconnection. Short conversations must not offer an empty older page.

Successful reads no longer wait for the terminal's activity status to match the record's completion state. Check Claude Code, Codex, OpenCode and Pi: open a conversation, let it finish, submit another prompt and confirm the next reply appears without a persistent record-catch-up warning. A blocked agent or an unfinished record is checked every three seconds while visible; a completed record with inactive terminal activity returns to the 30-second fallback. Each agent retains its own record completion rules. Update the test Companion as well as the phone; this change does not alter the public Latest installer.

Build 33 also fixes a home navigation race: swiping back can reveal home before the old space route is removed. A tap on the same agent or space during that interval now reopens it instead of being treated as already open. On the original iPhone, open an agent, swipe back from the left edge, then immediately tap that agent once; repeat with its space row. Both should open without a second tap. Check that a short, cancelled swipe stays in the pane and selecting the current tab keeps an unsent draft.

Test online chat, switch to the terminal and back, then disconnect and restart the app. Previously loaded home rows and recent chat text should remain readable. Queue text offline, restart again, reconnect and confirm it is delivered once. Images/files and TUI screens are not persisted offline. Unconfirmed in-flight sends are returned for user review, not automatically replayed.

Compare foreground/background behavior with background watching enabled and disabled. Hidden terminal output should stop consuming channel credit while keeping the phone's terminal width; independent status notifications and RPC remain available. This reduces work, but is not a measured battery-life claim.

**Known issue:** other intermittent first-tap symptoms on home, including after an uncaught JS error, remain unconfirmed. The swipe-back fix does not establish that they share its cause or are resolved. If a tap still fails, enable the relevant diagnostic categories, reproduce it and preview/share the report. This is a TestFlight test candidate, not approval of a public Release, Latest update or website claim.

## Request a build

After the maintainer completes the setup below, read the reviewed default-branch commit and dispatch **TestFlight** with that full SHA:

```sh
repo=jt-wang/herdr-anywhere
branch=$(gh api "repos/$repo" --jq .default_branch)
sha=$(gh api "repos/$repo/commits/$branch" --jq .sha)
gh workflow run testflight.yml -R "$repo" --ref "$branch" \
  -f expected_sha="$sha" -f version=0.2.0 -f build=26 -f upload=false
```

Choose the intended version and a new iOS build number. `upload=false` creates a signed IPA and checks its identity, resources, profile and signature locally. It makes no App Store Connect request.

For TestFlight delivery, use the same command with `-f upload=true`. This first validates the IPA with Apple, then submits it. Apple's processing and tester availability are subsequent steps; a successful upload does not mean processing has finished. This workflow does not submit an App Store release.

Find the matching SHA in the workflow runs, then watch its ID:

```sh
gh run list -R "$repo" --workflow testflight.yml --limit 5
gh run watch -R "$repo" RUN_ID --exit-status
```

A successful run provides `testflight-BUILD`, containing `Herdr.ipa`, `receipt.json` and `testflight-toolchain.json`. The receipt records the source SHA, version, build, IPA SHA-256, local check result and whether validation/upload succeeded. If the upload response is interrupted, check App Store Connect for that build before retrying.

With `upload=true`, a separate read-only `status` job then observes the exact version/build in App Store Connect for up to 20 minutes. Its `testflight-status-BUILD` artifact records processing, internal/external beta states, beta review state, app groups, build assignments and enabled public links. Upload success, processing `VALID` and tester availability are separate results. An enabled public link alone does not establish beta approval or installability; no groups or invitations are changed. The status job uses only the existing ASC credentials, never exports testers' names or email addresses, and retains its whitelisted JSON receipt for seven days.

If processing takes longer, rerun only the `status` job while the upload artifact remains available; do not rerun the archive job merely to refresh status. A status failure does not undo a confirmed upload. Both receipts must be checked before reporting delivery complete.

### Recover status using updated observer code

To inspect an already uploaded build with a newer, reviewed workflow, dispatch from the current default branch with `upload=false`, an explicit `status_run_id` and the full original `uploaded_sha`. `expected_sha` must still be the current reviewed default-branch commit, not the older upload commit. For the confirmed 0.2.1 (29) upload:

```sh
repo=jt-wang/herdr-anywhere
branch=$(gh api "repos/$repo" --jq .default_branch)
sha=$(gh api "repos/$repo/commits/$branch" --jq .sha)
gh workflow run testflight.yml -R "$repo" --ref "$branch" \
  -f expected_sha="$sha" -f version=0.2.1 -f build=29 -f upload=false \
  -f status_run_id=44239874593 \
  -f uploaded_sha=9919440fa7bbed8289250c089b558a21ee752cf3
```

Recovery keeps the request guard, skips the entire archive job and never validates/uploads an IPA again. Only recovery uses cross-run artifact download with the job's read-only Actions permission and GitHub token; it downloads `testflight-BUILD` from the specified run in this repository. The observer checks that its receipt has the exact original SHA, version and build and confirms both validation and upload before any ASC request. Missing/expired artifacts or mismatched receipts fail closed, with no rebuild/upload fallback. Recovery inputs must be supplied together; recovery with `upload=true` is rejected. Leave both inputs empty for the unchanged normal archive/upload flow.

The status receipt's `sha` identifies the original upload, `observer_sha` identifies the current executing code, and `status_run_id` records the recovery source run (null for a normal upload). HTTP failures expose only the HTTP status, allowlisted route and exact allowlisted machine codes (`FORBIDDEN_ERROR`, `NOT_AUTHORIZED`, `AUTHENTICATION_ERROR`), never response bodies, details, headers or arbitrary error strings. These codes help investigate authorization versus scope; a 403 alone does not establish which is responsible.

### Submit a build for Beta App Review

To give an already processed build to the Public group, dispatch from the default branch with `submit_beta=true`, `upload=false` and no `status_run_id`. The `beta` job finds the exact version and build. If the build has no beta review yet and its external state is `READY_FOR_BETA_SUBMISSION`, it adds the build to the external group named `Public` and creates a Beta App Review submission. It then reads the build back into the `testflight-beta-BUILD` artifact (groups, external state, beta review state). A build that already has a review (waiting, approved or rejected) is only read back. It never submits for App Store review and never changes other builds, groups or testers. Its only writes are POSTs to `/v1/betaGroups/{id}/relationships/builds` and `/v1/betaAppReviewSubmissions`, each with a two-minute token. App Store Connect accepts only GET requests in a token's scope (a POST-scoped token gets HTTP 405), so these two tokens carry no scope; the script refuses any other write route before it mints one. These need an API key whose role may manage TestFlight external testing (Admin or App Manager). A `FORBIDDEN_ERROR` on either route means the key's role lacks it. It runs in its own concurrency group, so it never replaces a pending upload.

```sh
gh workflow run testflight.yml -R "$repo" --ref "$branch" \
  -f expected_sha="$sha" -f version=0.3.5 -f build=40 -f upload=false -f submit_beta=true
```

## Maintainer setup

Create the **testflight** environment before adding credentials. Under **Deployment branches and tags**, choose **Selected branches and tags** and add only the repository's default **branch**, with its exact name. Do not add tag or pull-request patterns. Required reviewers are optional.

Store the following as **environment secrets**, rather than repository-wide secrets:

| Name | Value | Used for |
|---|---|---|
| `APPLE_TEAM_ID` | Signing team ID | Manual signing and profile matching |
| `IOS_DISTRIBUTION_P12_BASE64` | Base64 of the Apple Distribution certificate and its private key exported as p12 | Runner's temporary keychain |
| `IOS_DISTRIBUTION_P12_PASSWORD` | Password protecting that p12 | Importing the certificate and key |
| `IOS_PROVISION_PROFILE_BASE64` | Base64 of an unexpired App Store profile for `io.jingtao.herdranywhere` | Archive and export |
| `ASC_KEY_ID` | App Store Connect API key ID | Upload, read-only status and Beta App Review submission |
| `ASC_ISSUER_ID` | App Store Connect API issuer ID | Upload, read-only status and Beta App Review submission |
| `ASC_PRIVATE_KEY_P8` | Complete p8 PEM text, including its newlines | Upload, read-only status and Beta App Review submission; **not Base64** |

Use an existing App Store Connect app record with the bundle ID above. Exporting a p12 requires access to the certificate's private key; API authentication alone does not provide it.

The environment branch restriction enforces which refs may receive credentials. The workflow also requires `workflow_dispatch`, the default branch, matching workflow/source SHAs and the caller's exact `expected_sha`. A moving branch fails the SHA check instead of silently building another commit. Keep the default branch reviewed: changes to its workflow and release script run with signing access.

See GitHub's [environment branch restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments) and [Apple signing setup](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).

## Build and failure handling

The workflow verifies that its NativeShell CLI and iOS framework match the repository's five-patch key. `testflight-toolchain.json` records that key, the source SHA and hashes of both build inputs. Dependency downloads, Go/Rust builds and `--prepare-only` run before signing material is installed, in steps with none of the seven signing or ASC secrets. Preparation generates the store bundle and Xcode project, records their hashes with the framework inputs, and restores the source files. The signing step checks those hashes, the clean checkout SHA, version and build before importing the p12. It then archives for arm64 iOS devices, exports with manual signing and checks the IPA. Normal compilation of the reviewed Swift sources still happens during archive; codesign uses the temporary keychain. Symbol uploads are disabled, and an IPA containing Symbols or dSYM entries is rejected. Version and signing edits are temporary and restored afterward.

Only upload mode invokes `altool`; upload and read-only status write temporary p8 files. The status job mints a short-lived ES256 token for each request, scoped to that exact GET path and encoded query (including filters, fields and pagination), after validating the URL against its read-only endpoint allowlist. It reads groups through the app's beta-group relationship and each group's build IDs, without querying testers. It rejects redirects and off-origin pagination and removes its private signing file after each token. All helper subprocesses receive none of the seven raw signing or ASC secret environment variables. Team and profile identifiers are passed as signing arguments; `altool` receives its key identifiers as arguments and the temporary p8 through its private key directory. Signing files, the temporary keychain and installed profile are removed on normal completion, a caught failure, SIGINT or SIGTERM (keychain cleanup commands are bounded to three seconds each); GitHub-hosted runner disposal handles job termination. Use this workflow on the hosted runner specified in the file.

Missing configuration fails before dependency builds. Build, package checks, signing cleanup, validation or upload errors fail the run. Tool errors report the program and exit code without printing credential-bearing command lines. Artifacts contain only the IPA, receipt and toolchain record, not keychains, installed profiles, build folders or credentials.

The control-flow tests mock external commands and exercise real temporary files:

```sh
python3 app/scripts/testflight.test.py
python3 app/scripts/testflight-status.test.py
```

The first signed run must still verify the repository's configured certificate/profile and Apple's acceptance. Those credentials cannot be verified by the local mock tests.
