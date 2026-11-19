#!/usr/bin/env python3
"""Build and validate a manually signed TestFlight archive on a disposable runner."""
import argparse
import base64
import datetime
import hashlib
import json
import plistlib
import secrets
import signal
import shlex
import shutil
import tempfile
import uuid
import zipfile
import os
import re
import subprocess
from pathlib import Path

class ReleaseError(Exception):
    pass

def release(root, version, build, expected_sha, upload=False, env=None, request_only=False, config_only=False, prepare_only=False):
    env = dict(os.environ if env is None else env)
    default = env.get('TF_DEFAULT_BRANCH', '')
    ref = 'refs/heads/' + default
    repo = 'jt-wang/herdr-anywhere'
    if (not default or env.get('GITHUB_EVENT_NAME') != 'workflow_dispatch'
            or env.get('GITHUB_REPOSITORY') != repo or env.get('GITHUB_REF') != ref
            or not re.fullmatch('[0-9a-f]{40}', expected_sha)
            or env.get('GITHUB_SHA') != expected_sha or env.get('GITHUB_WORKFLOW_SHA') != expected_sha
            or env.get('GITHUB_WORKFLOW_REF') != repo + '/.github/workflows/testflight.yml@' + ref):
        raise ReleaseError('Trusted default-branch workflow and exact SHA required')
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', version) or not re.fullmatch('[1-9][0-9]*', build):
        raise ReleaseError('Use a three-part version and a positive build number')
    if request_only:
        return
    if prepare_only:
        return prepare(root, version, build, expected_sha, env)
    required = ('APPLE_TEAM_ID', 'IOS_DISTRIBUTION_P12_BASE64',
                'IOS_DISTRIBUTION_P12_PASSWORD', 'IOS_PROVISION_PROFILE_BASE64')
    if upload:
        required += ('ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8')
    missing = [name for name in required if not env.get(name)]
    if missing:
        raise ReleaseError('Missing configuration: ' + ', '.join(missing))
    if config_only:
        return
    return archive(root, version, build, expected_sha, upload, env)

def command(args, env, timeout=None):
    child_env = {k: v for k, v in env.items() if k != 'APPLE_TEAM_ID' and not k.startswith(('ASC_', 'IOS_DISTRIBUTION_', 'IOS_PROVISION_'))}
    if list(args[:2]) != ['xcrun', 'altool']:
        child_env.pop('API_PRIVATE_KEYS_DIR', None)
    result = subprocess.run([str(a) for a in args], env=child_env, capture_output=True, timeout=timeout)
    if result.returncode:
        # Tool output and argv may contain signing data. Report only the program and exit code.
        raise ReleaseError(f'{Path(str(args[0])).name} failed (exit {result.returncode})')
    return result.stdout + result.stderr


def profile_data(path, env, bundle):
    data = plistlib.loads(command(['security', 'cms', '-D', '-i', path], env))
    ent = data.get('Entitlements', {})
    team = env['APPLE_TEAM_ID']
    if (data.get('TeamIdentifier') != [team]
            or ent.get('application-identifier') != team + '.' + bundle
            or ent.get('get-task-allow') is not False or ent.get('beta-reports-active') is not True
            or 'ProvisionedDevices' in data or data.get('ProvisionsAllDevices')
            or data['ExpirationDate'] <= datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)):
        raise ReleaseError('An unexpired App Store profile for this team and bundle is required')
    uuid.UUID(data['UUID'])
    return data


def private_file(path, data):
    with path.open('xb') as f:
        path.chmod(0o600)
        f.write(data)


def check_ipa(ipa, folder, env, bundle, version, build, sounds):
    with zipfile.ZipFile(ipa) as z:
        for name in z.namelist():
            if 'Symbols' in Path(name).parts or any(p.endswith('.dSYM') for p in Path(name).parts):
                raise ReleaseError('Symbols must not be included in the IPA')
            if Path(name).is_absolute() or '..' in Path(name).parts:
                raise ReleaseError('Unexpected IPA entry')
        z.extractall(folder)
    apps = list((folder / 'Payload').glob('*.app'))
    if len(apps) != 1:
        raise ReleaseError('Expected one app in the IPA')
    app = apps[0]
    info = plistlib.loads((app / 'Info.plist').read_bytes())
    expected = {'CFBundleIdentifier': bundle, 'CFBundleShortVersionString': version,
                'CFBundleVersion': build, 'DTPlatformName': 'iphoneos', 'ITSAppUsesNonExemptEncryption': False}
    if any(info.get(k) != v for k, v in expected.items()):
        raise ReleaseError('IPA identity, version, build or device metadata mismatch')
    if not info.get('CFBundleIcons', {}).get('CFBundlePrimaryIcon', {}).get('CFBundleIconName'):
        raise ReleaseError('IPA has no app icon')
    manifest = plistlib.loads((app / 'PrivacyInfo.xcprivacy').read_bytes())
    binary = app / info['CFBundleExecutable']
    undefined = command(['nm', '-u', binary], env).decode(errors='replace')
    categories = {p['NSPrivacyAccessedAPIType'] for p in manifest.get('NSPrivacyAccessedAPITypes', [])}
    for pattern, category in [(r'_(?:stat|fstat|fstatat|lstat)(?:\$|\s|$)', 'FileTimestamp'),
                              (r'_mach_absolute_time', 'SystemBootTime'), (r'NSUserDefaults', 'UserDefaults')]:
        if re.search(pattern, undefined) and 'NSPrivacyAccessedAPICategory' + category not in categories:
            raise ReleaseError('IPA is missing a required API privacy declaration')
    if not (app / 'nsh/bundle.nshc').is_file() or (app / 'nsh/bundle.js').exists():
        raise ReleaseError('IPA must contain compiled app bytecode without JS source')
    for sound in sounds:
        if not (app / 'nsh/assets/assets/sounds' / sound.name).is_file():
            raise ReleaseError('IPA is missing a sound resource')
    profile_data(app / 'embedded.mobileprovision', env, bundle)
    if b'Authority=Apple Distribution' not in command(['codesign', '-dvv', app], env):
        raise ReleaseError('IPA must be signed with Apple Distribution')
    command(['codesign', '--verify', '--deep', '--strict', app], env)
    for f in app.rglob('*'):
        if f.is_file():
            for match in re.finditer(rb'/(Users|Volumes)/([^/\s\x00-\x1f\x7f]{1,64})', f.read_bytes()):
                if not (match[1] == b'Users' and match[2] in (b'runner', b'sam')):
                    raise ReleaseError('IPA contains a private build path')
    for framework in (app / 'Frameworks').glob('*.framework'):
        binary = framework / framework.stem
        if command(['lipo', '-archs', binary], env).strip() != b'arm64':
            raise ReleaseError('IPA framework is not arm64-only')
        if not re.search(rb'platform IOS\s*$', command(['vtool', '-show-build', binary], env), re.M):
            raise ReleaseError('IPA framework is not built for iOS devices')


def check_source(root, sha, env):
    actual = command(['git', '-C', root, 'rev-parse', 'HEAD'], env).decode().strip()
    if actual != sha:
        raise ReleaseError('Prepared source SHA mismatch')
    command(['git', '-C', root, 'diff', '--quiet', 'HEAD'], env)


def prepared_inputs(root):
    paths = ['app/ios/Herdr.xcodeproj', 'app/ios/nsh', 'app/ios/Info.plist',
             'third_party/tailpocket-342ebe5/build/apple/TailPocketMobile.xcframework',
             'third_party/tailpocket-342ebe5/sdk/apple/NSH/NativeShell.xcframework']
    for name in ['app/ios/Herdr.xcodeproj/project.pbxproj', 'app/ios/nsh/bundle.nshc',
                 'third_party/tailpocket-342ebe5/sdk/apple/NSH/NativeShell.xcframework/ios-arm64/NativeShell.framework/NativeShell']:
        if not (root / name).is_file():
            raise ReleaseError('Missing prepared input: ' + name)
    result = {}
    for name in paths:
        path = root / name
        files = sorted(p for p in path.rglob('*') if p.is_file()) if path.is_dir() else [path]
        if not files or not path.exists():
            raise ReleaseError('Missing prepared input: ' + name)
        for file in files:
            result[file.relative_to(root).as_posix()] = hashlib.sha256(file.read_bytes()).hexdigest()
    return result


def prepare(root, version, build, sha, env):
    root = Path(root).resolve()
    ios = root / 'app/ios'
    marker = ios / 'build/testflight-prepared.json'
    marker.parent.mkdir(parents=True, exist_ok=True)
    marker.unlink(missing_ok=True)
    check_source(root, sha, env)
    project, config, channel = ios / 'project.yml', root / 'app/nsh.json', root / 'app/src/channel.ts'
    originals = {p: p.read_bytes() for p in (project, config, channel)}
    try:
        spec = originals[project].decode()
        for name, value in [('MARKETING_VERSION', version), ('CURRENT_PROJECT_VERSION', build)]:
            spec, count = re.subn(r'(?m)^(\s*' + name + r':) .*$', r'\g<1> "' + value + '"', spec)
            if count != 1:
                raise ReleaseError('Expected one version setting in project.yml')
        project.write_text(spec)
        nsh = json.loads(originals[config]); nsh['version'] = version
        config.write_text(json.dumps(nsh, indent=2) + '\n')
        source, count = re.subn(r"(CHANNEL: 'source' \| 'store' = )'source'", r"\1'store'", originals[channel].decode())
        if count != 1:
            raise ReleaseError('Expected the source update channel')
        channel.write_text(source)
        command([ios / 'build.sh', 'App.tsx', 'prepare'], env)
        record = {'sha': sha, 'version': version, 'build': build, 'files': prepared_inputs(root)}
    finally:
        for path, data in originals.items():
            path.write_bytes(data)
    marker.write_text(json.dumps(record, indent=2) + '\n')
    return marker


def verify_prepared(root, version, build, sha, env):
    check_source(root, sha, env)
    try:
        record = json.loads((root / 'app/ios/build/testflight-prepared.json').read_text())
    except (OSError, ValueError) as exc:
        raise ReleaseError('Missing or invalid prepared receipt') from exc
    if (record.get('sha'), record.get('version'), record.get('build')) != (sha, version, build):
        raise ReleaseError('Prepared request mismatch')
    if record.get('files') != prepared_inputs(root):
        raise ReleaseError('Prepared input hash mismatch')


def archive(root, version, build, sha, upload, env):
    root = Path(root).resolve()
    if not re.fullmatch('[A-Z0-9]{10}', env['APPLE_TEAM_ID']):
        raise ReleaseError('Invalid team identifier')
    if upload:
        if not re.fullmatch('[A-Za-z0-9]+', env['ASC_KEY_ID']):
            raise ReleaseError('Invalid key identifier')
        uuid.UUID(env['ASC_ISSUER_ID'])
    verify_prepared(root, version, build, sha, env)
    ios = root / 'app/ios'
    output = ios / 'build/testflight'
    output.mkdir(parents=True, exist_ok=False)
    project, config, channel, signing = ios / 'project.yml', root / 'app/nsh.json', root / 'app/src/channel.ts', ios / 'Signing.local.xcconfig'
    originals = {p: p.read_bytes() if p.exists() else None for p in (project, config, channel, signing)}
    installed = None
    keychain = None
    search = None
    cleanup_errors = []
    with tempfile.TemporaryDirectory(prefix='testflight-', dir=env['RUNNER_TEMP']) as work_name:
        work = Path(work_name)
        try:
            p12, mobile = work / 'distribution.p12', work / 'profile.mobileprovision'
            private_file(p12, base64.b64decode(env['IOS_DISTRIBUTION_P12_BASE64'], validate=True))
            private_file(mobile, base64.b64decode(env['IOS_PROVISION_PROFILE_BASE64'], validate=True))
            bundle = json.loads(originals[config])['id']
            profile = profile_data(mobile, env, bundle)
            profiles = Path(env['HOME']) / 'Library/MobileDevice/Provisioning Profiles'
            profiles.mkdir(parents=True, exist_ok=True)
            destination = profiles / (profile['UUID'] + '.mobileprovision')
            private_file(destination, mobile.read_bytes())
            installed = destination
            search = shlex.split(command(['security', 'list-keychains', '-d', 'user'], env).decode())
            keychain = work / 'signing.keychain-db'
            password = secrets.token_hex(24)
            command(['security', 'create-keychain', '-p', password, keychain], env)
            command(['security', 'set-keychain-settings', '-lut', '21600', keychain], env)
            command(['security', 'unlock-keychain', '-p', password, keychain], env)
            command(['security', 'import', p12, '-k', keychain, '-P', env['IOS_DISTRIBUTION_P12_PASSWORD'],
                     '-T', '/usr/bin/codesign', '-T', '/usr/bin/security'], env)
            command(['security', 'set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:', '-s', '-k', password, keychain], env)
            command(['security', 'list-keychains', '-d', 'user', '-s', keychain, *search], env)
            signing.write_text('DEVELOPMENT_TEAM = ' + env['APPLE_TEAM_ID'] + '\nCODE_SIGN_STYLE = Manual\n')
            archive_path = output / 'Herdr.xcarchive'
            command(['xcodebuild', '-quiet', '-project', ios / 'Herdr.xcodeproj', '-scheme', 'Herdr',
                     '-configuration', 'Release', '-destination', 'generic/platform=iOS', 'ARCHS=arm64',
                     '-derivedDataPath', output / 'derived', '-clonedSourcePackagesDirPath', output / 'spm',
                     '-packageCachePath', output / 'spm-cache', '-archivePath', archive_path,
                     'DEVELOPMENT_TEAM=' + env['APPLE_TEAM_ID'], 'CODE_SIGN_STYLE=Manual',
                     'CODE_SIGN_IDENTITY=Apple Distribution', 'PROVISIONING_PROFILE_SPECIFIER=' + profile['UUID'], 'archive'], env)
            options = work / 'ExportOptions.plist'
            options.write_bytes(plistlib.dumps({'method': 'app-store-connect', 'destination': 'export',
                'teamID': env['APPLE_TEAM_ID'], 'signingStyle': 'manual', 'signingCertificate': 'Apple Distribution',
                'provisioningProfiles': {bundle: profile['UUID']}, 'uploadSymbols': False, 'manageAppVersionAndBuildNumber': False}))
            command(['xcodebuild', '-quiet', '-exportArchive', '-archivePath', archive_path,
                     '-exportOptionsPlist', options, '-exportPath', output / 'export'], env)
            ipa = output / 'export/Herdr.ipa'
            check_ipa(ipa, work / 'checked', env, bundle, version, build, (root / 'app/assets/sounds').glob('*.mp3'))
            if upload:
                keys = work / 'keys'; keys.mkdir(mode=0o700)
                private_file(keys / ('AuthKey_' + env['ASC_KEY_ID'] + '.p8'), env['ASC_PRIVATE_KEY_P8'].encode())
                env = dict(env, API_PRIVATE_KEYS_DIR=str(keys))
                auth = ['-f', ipa, '-t', 'ios', '--apiKey', env['ASC_KEY_ID'], '--apiIssuer', env['ASC_ISSUER_ID']]
                command(['xcrun', 'altool', '--validate-app', *auth], env)
                command(['xcrun', 'altool', '--upload-app', *auth], env)
            (output / 'receipt.json').write_text(json.dumps({'sha': sha, 'version': version, 'build': build,
                'local_checked': True, 'validated': upload, 'uploaded': upload, 'ipa_sha256': hashlib.sha256(ipa.read_bytes()).hexdigest()}, indent=2) + '\n')
        finally:
            # Attempt every cleanup even if restoring one resource fails.
            actions = []
            if search is not None:
                actions.append(lambda: command(['security', 'list-keychains', '-d', 'user', '-s', *search], env, timeout=3))
            if keychain is not None:
                actions.append(lambda: command(['security', 'delete-keychain', keychain], env, timeout=3))
            if installed is not None:
                actions.append(lambda: installed.unlink(missing_ok=True))
            for path, data in originals.items():
                actions.append(lambda p=path, d=data: p.unlink(missing_ok=True) if d is None else p.write_bytes(d))
            for action in actions:
                try:
                    action()
                except Exception:
                    cleanup_errors.append('Signing cleanup failed')
            if cleanup_errors:
                raise ReleaseError('; '.join(cleanup_errors))
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--build', required=True)
    parser.add_argument('--expected-sha', required=True)
    parser.add_argument('--upload', action='store_true')
    parser.add_argument('--request-only', action='store_true')
    parser.add_argument('--config-only', action='store_true')
    parser.add_argument('--prepare-only', action='store_true')
    args = parser.parse_args()
    def cancelled(signum, frame):
        # Let the first signal unwind the archive and finish bounded cleanup.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise ReleaseError('TestFlight cancelled')
    previous = {sig: signal.signal(sig, cancelled) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        release(Path(__file__).resolve().parents[2], args.version, args.build, args.expected_sha,
                args.upload, request_only=args.request_only, config_only=args.config_only, prepare_only=args.prepare_only)
    except Exception as exc:
        print(str(exc) if isinstance(exc, ReleaseError) else 'TestFlight failed: ' + type(exc).__name__)
        return 1
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
