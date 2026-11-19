# Build Herdr for TestFlight

A cloud coding agent can request an iOS build through GitHub Actions. Apple signing credentials stay in the repository's `testflight` environment; the agent needs a GitHub token with **Actions: write** and **Contents: read**.

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

## Maintainer setup

Create the **testflight** environment before adding credentials. Under **Deployment branches and tags**, choose **Selected branches and tags** and add only the repository's default **branch**, with its exact name. Do not add tag or pull-request patterns. Required reviewers are optional.

Store the following as **environment secrets**, rather than repository-wide secrets:

| Name | Value | Used for |
|---|---|---|
| `APPLE_TEAM_ID` | Signing team ID | Manual signing and profile matching |
| `IOS_DISTRIBUTION_P12_BASE64` | Base64 of the Apple Distribution certificate and its private key exported as p12 | Runner's temporary keychain |
| `IOS_DISTRIBUTION_P12_PASSWORD` | Password protecting that p12 | Importing the certificate and key |
| `IOS_PROVISION_PROFILE_BASE64` | Base64 of an unexpired App Store profile for `io.jingtao.herdranywhere` | Archive and export |
| `ASC_KEY_ID` | App Store Connect API key ID | Upload and read-only status |
| `ASC_ISSUER_ID` | App Store Connect API issuer ID | Upload and read-only status |
| `ASC_PRIVATE_KEY_P8` | Complete p8 PEM text, including its newlines | Upload and read-only status; **not Base64** |

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
