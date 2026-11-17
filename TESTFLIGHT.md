# Build Herdr for TestFlight

A cloud coding agent can request an iOS build through GitHub Actions. Apple signing credentials stay in the repository's `testflight` environment; the agent needs a GitHub token with **Actions: write** and **Contents: read**.

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

## Maintainer setup

Create the **testflight** environment before adding credentials. Under **Deployment branches and tags**, choose **Selected branches and tags** and add only the repository's default **branch**, with its exact name. Do not add tag or pull-request patterns. Required reviewers are optional.

Store the following as **environment secrets**, rather than repository-wide secrets:

| Name | Value | Used for |
|---|---|---|
| `APPLE_TEAM_ID` | Signing team ID | Manual signing and profile matching |
| `IOS_DISTRIBUTION_P12_BASE64` | Base64 of the Apple Distribution certificate and its private key exported as p12 | Runner's temporary keychain |
| `IOS_DISTRIBUTION_P12_PASSWORD` | Password protecting that p12 | Importing the certificate and key |
| `IOS_PROVISION_PROFILE_BASE64` | Base64 of an unexpired App Store profile for `io.jingtao.herdranywhere` | Archive and export |
| `ASC_KEY_ID` | App Store Connect API key ID | Upload mode only |
| `ASC_ISSUER_ID` | App Store Connect API issuer ID | Upload mode only |
| `ASC_PRIVATE_KEY_P8` | Complete p8 PEM text, including its newlines | Upload mode only; **not Base64** |

Use an existing App Store Connect app record with the bundle ID above. Exporting a p12 requires access to the certificate's private key; API authentication alone does not provide it.

The environment branch restriction enforces which refs may receive credentials. The workflow also requires `workflow_dispatch`, the default branch, matching workflow/source SHAs and the caller's exact `expected_sha`. A moving branch fails the SHA check instead of silently building another commit. Keep the default branch reviewed: changes to its workflow and release script run with signing access.

See GitHub's [environment branch restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments) and [Apple signing setup](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).

## Build and failure handling

The workflow verifies that its NativeShell CLI and iOS framework match the repository's five-patch key. `testflight-toolchain.json` records that key, the source SHA and hashes of both build inputs. Dependency downloads, Go/Rust builds and `--prepare-only` run before signing material is installed, in steps with none of the seven signing or ASC secrets. Preparation generates the store bundle and Xcode project, records their hashes with the framework inputs, and restores the source files. The signing step checks those hashes, the clean checkout SHA, version and build before importing the p12. It then archives for arm64 iOS devices, exports with manual signing and checks the IPA. Normal compilation of the reviewed Swift sources still happens during archive; codesign uses the temporary keychain. Symbol uploads are disabled, and an IPA containing Symbols or dSYM entries is rejected. Version and signing edits are temporary and restored afterward.

Only upload mode writes a temporary p8 and invokes `altool`. All helper subprocesses receive none of the seven raw signing or ASC secret environment variables. Team and profile identifiers are passed as signing arguments; `altool` receives its key identifiers as arguments and the temporary p8 through its private key directory. Signing files, the temporary keychain and installed profile are removed on normal completion, a caught failure, SIGINT or SIGTERM (keychain cleanup commands are bounded to three seconds each); GitHub-hosted runner disposal handles job termination. Use this workflow on the hosted runner specified in the file.

Missing configuration fails before dependency builds. Build, package checks, signing cleanup, validation or upload errors fail the run. Tool errors report the program and exit code without printing credential-bearing command lines. Artifacts contain only the IPA, receipt and toolchain record, not keychains, installed profiles, build folders or credentials.

The control-flow tests mock external commands and exercise real temporary files:

```sh
python3 app/scripts/testflight.test.py
```

The first signed run must still verify the repository's configured certificate/profile and Apple's acceptance. Those credentials cannot be verified by the local mock tests.
