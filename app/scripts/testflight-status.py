#!/usr/bin/env python3
"""Read-only ASC receipt after a confirmed upload; never distribute or submit a build."""
import argparse
import base64
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


ORIGIN = 'https://api.appstoreconnect.apple.com'
BUNDLE = 'io.jingtao.herdranywhere'
ROUTES = ('/v1/apps', '/v1/builds')
# Exact machine codes only: never echo arbitrary server strings or code suffixes.
ERROR_CODES = frozenset(('FORBIDDEN_ERROR', 'NOT_AUTHORIZED', 'AUTHENTICATION_ERROR',
                         'PARAMETER_ERROR.INVALID', 'PARAMETER_ERROR.UNKNOWN',
                         'PARAMETER_ERROR.INVALID_COMBINATION', 'PARAMETER_ERROR.INVALID_VALUE',
                         'STATE_ERROR', 'ENTITY_ERROR.RELATIONSHIP.INVALID', 'ENTITY_ERROR.ATTRIBUTE.REQUIRED'))
ERROR_PARAMETERS = frozenset(('filter[app]', 'filter[builds]', 'fields[betaGroups]'))
# The only writes, used by testflight-beta.py: add a build to a group, submit a build for Beta App Review.
WRITE_ROUTES = r'/v1/(?:betaAppReviewSubmissions|betaGroups/[A-Za-z0-9-]+/relationships/builds)'


class StatusError(Exception):
    pass


def b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode('ascii')


def raw_signature(der):
    # OpenSSL emits ASN.1 DER, but ES256 JWT requires fixed-width R || S.
    if len(der) < 8 or der[0] != 0x30 or der[1] != len(der) - 2:
        raise StatusError('Invalid ES256 signature')
    parts, pos = [], 2
    for _ in range(2):
        if pos + 2 > len(der) or der[pos] != 2:
            raise StatusError('Invalid ES256 signature')
        size = der[pos + 1]
        value = der[pos + 2:pos + 2 + size]
        if not value or len(value) != size or value[0] & 0x80:
            raise StatusError('Invalid ES256 signature')
        value = value.lstrip(b'\0')
        if len(value) > 32:
            raise StatusError('Invalid ES256 signature')
        parts.append(value.rjust(32, b'\0'))
        pos += 2 + size
    if pos != len(der):
        raise StatusError('Invalid ES256 signature')
    return b''.join(parts)


def token(env, url, method='GET'):
    # https://developer.apple.com/documentation/appstoreconnectapi/generating-tokens-for-api-requests
    # Team key, ES256, 2-minute life. A GET is scoped to its exact path and query. App Store Connect takes only GET
    # entries in a scope and answers a POST-scoped token with 405, so a write's token has no scope; safe_url has
    # already limited writes to the two allowlisted routes.
    parsed = urllib.parse.urlsplit(safe_url(url, method))
    target = parsed.path + ('?' + parsed.query if parsed.query else '')
    now = int(time.time())
    header = {'alg': 'ES256', 'kid': env['ASC_KEY_ID'], 'typ': 'JWT'}
    payload = {'iss': env['ASC_ISSUER_ID'], 'iat': now, 'exp': now + 120, 'aud': 'appstoreconnect-v1'}
    if method == 'GET':
        payload['scope'] = ['GET ' + target]
    message = '.'.join(b64(json.dumps(p, separators=(',', ':')).encode()) for p in (header, payload))
    child_env = {k: v for k, v in env.items() if k != 'APPLE_TEAM_ID'
                 and k != 'API_PRIVATE_KEYS_DIR'
                 and not k.startswith(('ASC_', 'IOS_DISTRIBUTION_', 'IOS_PROVISION_'))}
    with tempfile.TemporaryDirectory(prefix='asc-status-', dir=env.get('RUNNER_TEMP')) as folder:
        key = Path(folder) / 'key.p8'
        with key.open('xb') as f:
            key.chmod(0o600)
            f.write(env['ASC_PRIVATE_KEY_P8'].encode())
        result = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(key)],
                                input=message.encode(), capture_output=True, env=child_env, timeout=10)
        if result.returncode:
            raise StatusError('JWT signing failed (openssl exit ' + str(result.returncode) + ')')
        signature = raw_signature(result.stdout)
    return message + '.' + b64(signature)


def safe_url(url, method='GET'):
    parsed = urllib.parse.urlsplit(url)
    if method == 'POST':
        if (parsed.scheme != 'https' or parsed.netloc != 'api.appstoreconnect.apple.com' or parsed.query
                or parsed.fragment or not re.fullmatch(WRITE_ROUTES, parsed.path)):
            raise StatusError('Unsafe ASC write URL rejected')
        return url
    if method != 'GET':
        raise StatusError('Unsafe ASC method rejected')
    group_relation = re.fullmatch(r'/v1/(?:apps/[A-Za-z0-9-]+/betaGroups|betaGroups/[A-Za-z0-9-]+/relationships/builds)',
                                 parsed.path)
    if (parsed.scheme != 'https' or parsed.netloc != 'api.appstoreconnect.apple.com'
            or (parsed.path not in ROUTES and not group_relation) or parsed.fragment):
        raise StatusError('Unsafe ASC pagination URL rejected')
    return url


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise StatusError('ASC redirect rejected')


class Client:
    def __init__(self, env, deadline):
        self.env, self.deadline = env, deadline
        self.opener = urllib.request.build_opener(NoRedirect())

    def get(self, url):
        return self.send('GET', url)

    def send(self, method, url, body=None):
        safe_url(url, method)  # Validate before minting or attaching a bearer token.
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise StatusError('ASC observation timed out')
        headers = {'Authorization': 'Bearer ' + token(self.env, url, method), 'Accept': 'application/json'}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        request = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=min(30, remaining)) as response:
                raw = response.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as exc:
            # Bounded parsing; only exact allowlisted machine codes may leave here.
            codes, parameters = set(), set()
            try:
                body = exc.read(65537)
                data = json.loads(body) if len(body) <= 65536 else None
                errors = data.get('errors') if isinstance(data, dict) else None
                if isinstance(errors, list):
                    for error in errors:
                        code = error.get('code') if isinstance(error, dict) else None
                        if isinstance(code, str) and code in ERROR_CODES:
                            codes.add(code)
                        source = error.get('source') if isinstance(error, dict) else None
                        parameter = source.get('parameter') if isinstance(source, dict) else None
                        if isinstance(parameter, str) and parameter in ERROR_PARAMETERS:
                            parameters.add(parameter)
            except (ValueError, OSError, TimeoutError, RecursionError):
                pass
            finally:
                exc.close()
            suffix = ' (codes: ' + ', '.join(sorted(codes)) + ')' if codes else ''
            if parameters:
                suffix += ' (parameters: ' + ', '.join(sorted(parameters)) + ')'
            raise StatusError('ASC HTTP ' + str(exc.code) + ' on ' + urllib.parse.urlsplit(url).path + suffix) from None
        except (urllib.error.URLError, TimeoutError):
            raise StatusError('ASC network error or request timeout') from None

    def listing(self, route, params):
        url = ORIGIN + route + ('?' + urllib.parse.urlencode(params) if params else '')
        data, included, visited = [], [], set()
        while url:
            if url in visited or len(visited) >= 100:
                raise StatusError('ASC pagination cycle or page limit')
            visited.add(url)
            page = self.get(safe_url(url))
            data.extend(page['data'])
            included.extend(page.get('included', []))
            url = page.get('links', {}).get('next')
        return data, included


def related(item, name, included):
    link = item.get('relationships', {}).get(name, {}).get('data')
    if not isinstance(link, dict):
        return {}
    return next((r for r in included if (r['type'], r['id']) == (link['type'], link['id'])), {})


def enum(value):
    # State strings only, not arbitrary server text or error descriptions.
    return value if isinstance(value, str) and re.fullmatch('[A-Z][A-Z0-9_]{0,79}', value) else None


def exact_build(items, included, app_id, version, build):
    matches = []
    for item in items:
        pre = related(item, 'preReleaseVersion', included).get('attributes', {})
        app = item.get('relationships', {}).get('app', {}).get('data', {})
        if (item['type'] == 'builds' and item.get('attributes', {}).get('version') == build
                and pre.get('version') == version and pre.get('platform') == 'IOS'
                and app.get('id', app_id) == app_id):
            matches.append(item)
    if len(matches) > 1:
        raise StatusError('Multiple exact iOS builds returned by ASC')
    return matches[0] if matches else None


def groups(client, app_id, build_id):
    # Read ownership relationships directly; the collection filter query is rejected by ASC.
    all_groups, _ = client.listing('/v1/apps/' + app_id + '/betaGroups', {})
    result = []
    for group in all_groups:
        builds, _ = client.listing('/v1/betaGroups/' + group['id'] + '/relationships/builds', {})
        attrs = group.get('attributes', {})
        internal = attrs.get('isInternalGroup') is True
        all_builds = attrs.get('hasAccessToAllBuilds') is True
        explicit = any(b['type'] == 'builds' and b['id'] == build_id for b in builds)
        # Internal automatic groups can have access without an explicit build assignment.
        access = explicit or (internal and all_builds)
        link = attrs.get('publicLink')
        valid_link = isinstance(link, str) and re.fullmatch(r'https://testflight\.apple\.com/join/[A-Za-z0-9]+', link)
        enabled = attrs.get('publicLinkEnabled') is True
        result.append({'id': group['id'], 'name': attrs.get('name'), 'is_internal': internal,
                       'has_access_to_all_builds': all_builds, 'assigned_to_build': explicit,
                       'has_build_access': access, 'public_link_enabled': enabled,
                       'public_link': link if valid_link else None,
                       'public_invitation_for_build': bool(access and not internal and enabled and valid_link)})
    return result


def observe(client, version, build, receipt, deadline, interval=30):
    apps, _ = client.listing('/v1/apps', {'filter[bundleId]': BUNDLE, 'fields[apps]': 'bundleId'})
    apps = [a for a in apps if a.get('attributes', {}).get('bundleId') == BUNDLE]
    if len(apps) != 1:
        raise StatusError('Expected exactly one ASC app for bundle ID')
    app_id = apps[0]['id']
    receipt['app_id'] = app_id
    params = {'filter[app]': app_id, 'filter[version]': build,
              'filter[preReleaseVersion.version]': version, 'filter[preReleaseVersion.platform]': 'IOS',
              'include': 'buildBetaDetail,preReleaseVersion,betaAppReviewSubmission',
              'fields[buildBetaDetails]': 'internalBuildState,externalBuildState'}
    while True:
        if time.monotonic() >= deadline:
            raise StatusError('ASC processing observation timed out')
        items, included = client.listing('/v1/builds', params)
        item = exact_build(items, included, app_id, version, build)
        if item:
            attrs = item.get('attributes', {})
            state = enum(attrs.get('processingState'))
            detail = related(item, 'buildBetaDetail', included).get('attributes', {})
            review = related(item, 'betaAppReviewSubmission', included).get('attributes', {})
            receipt.update({'build_id': item['id'], 'processing_state': state,
                            'processing_valid': state == 'VALID',
                            'internal_build_state': enum(detail.get('internalBuildState')),
                            'external_build_state': enum(detail.get('externalBuildState')),
                            'beta_review_state': enum(review.get('betaReviewState'))})
            if state == 'VALID':
                receipt['groups'] = groups(client, app_id, item['id'])
                receipt['public_invitation_for_build'] = any(g['public_invitation_for_build'] for g in receipt['groups'])
                # Enumerate every page, without a version/state filter: an older version's
                # build number or an invalid/expired upload can still be occupied.
                items, included = client.listing('/v1/builds', {
                    'filter[app]': app_id, 'filter[preReleaseVersion.platform]': 'IOS',
                    'include': 'preReleaseVersion', 'limit': '200'})
                builds = []
                for candidate in items:
                    pre = related(candidate, 'preReleaseVersion', included).get('attributes', {})
                    attrs = candidate.get('attributes', {})
                    number, release_version = attrs.get('version'), pre.get('version')
                    if (candidate.get('type') != 'builds' or pre.get('platform') != 'IOS'
                            or not isinstance(number, str) or not re.fullmatch(r'[0-9]+(?:\.[0-9]+){0,2}', number)
                            or not isinstance(release_version, str)
                            or not re.fullmatch(r'[0-9]+(?:\.[0-9]+){0,2}', release_version)):
                        raise StatusError('ASC build inventory contains an unexpected identity')
                    builds.append({'id': candidate['id'], 'version': release_version, 'build': number,
                                   'processing_state': enum(attrs.get('processingState'))})
                receipt['build_inventory'] = {
                    'queried_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                    'builds': builds,
                    'highest_numeric_build': max((int(b['build']) for b in builds if b['build'].isdigit()), default=None),
                    'build_30_occupied': any(b['build'] == '30' for b in builds)}
                return
            if state != 'PROCESSING':
                raise StatusError('ASC build processing failed or returned an unknown state')
        time.sleep(min(interval, max(0, deadline - time.monotonic())))


def run(args, env):
    # Reuse the existing exact-SHA/default-branch guard without signing or uploading.
    spec = importlib.util.spec_from_file_location('testflight', Path(__file__).with_name('testflight.py'))
    release = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(release)
    release.release(Path(__file__).resolve().parents[2], args.version, args.build, args.expected_sha,
                    env=env, request_only=True)
    if bool(args.status_run_id) != bool(args.uploaded_sha):
        raise StatusError('Status recovery requires both status_run_id and uploaded_sha')
    if args.status_run_id and (not re.fullmatch('[1-9][0-9]*', args.status_run_id)
                               or not re.fullmatch('[0-9a-f]{40}', args.uploaded_sha)):
        raise StatusError('Status recovery requires a positive run ID and full uploaded SHA')
    if args.status_run_id and env.get('TF_UPLOAD') == 'true':
        raise StatusError('Status recovery cannot upload')
    if args.request_only:
        return
    uploaded_sha = args.uploaded_sha or args.expected_sha
    paths = list(Path(args.upload_artifact).rglob('receipt.json'))
    if len(paths) != 1:
        raise StatusError('Expected one archive upload receipt')
    uploaded = json.loads(paths[0].read_text())
    if (any(uploaded.get(k) != v for k, v in {'sha': uploaded_sha, 'version': args.version,
                                           'build': args.build}.items())
            or uploaded.get('uploaded') is not True or uploaded.get('validated') is not True):
        raise StatusError('Archive receipt does not confirm this exact upload')
    receipt = {'sha': uploaded_sha, 'observer_sha': args.expected_sha, 'status_run_id': args.status_run_id or None,
               'bundle_id': BUNDLE, 'version': args.version, 'build': args.build,
               'uploaded': True, 'processing_state': None, 'processing_valid': False,
               'internal_build_state': None, 'external_build_state': None, 'beta_review_state': None,
               'groups': None, 'public_invitation_for_build': None, 'observation': 'error',
               'distribution_note': 'Public invitation means an enabled link on an associated external group; '
                                    'it does not prove beta approval, tester eligibility, or installability.'}
    try:
        missing = [k for k in ('ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8') if not env.get(k)]
        if missing:
            raise StatusError('Missing ASC configuration: ' + ', '.join(missing))
        deadline = time.monotonic() + args.timeout
        observe(Client(env, deadline), args.version, args.build, receipt, deadline)
        receipt['observation'] = 'complete'
    except StatusError as exc:
        receipt['error'] = str(exc)
        raise
    except Exception as exc:
        receipt['error'] = 'ASC observation failed: ' + type(exc).__name__
        raise StatusError(receipt['error']) from None
    finally:
        # Only explicitly constructed fields, never raw Apple responses or upload metadata.
        Path(args.output).write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--build', required=True)
    parser.add_argument('--expected-sha', required=True)
    parser.add_argument('--status-run-id', default='')
    parser.add_argument('--uploaded-sha', default='')
    parser.add_argument('--request-only', action='store_true')
    parser.add_argument('--upload-artifact')
    parser.add_argument('--output')
    parser.add_argument('--timeout', type=int, default=1200, choices=range(1, 1201), metavar='1..1200')
    args = parser.parse_args()
    if not args.request_only and (not args.upload_artifact or not args.output):
        parser.error('--upload-artifact and --output are required for observation')
    def cancelled(signum, frame):
        raise StatusError('ASC observation cancelled')
    previous = {sig: signal.signal(sig, cancelled) for sig in (signal.SIGINT, signal.SIGTERM)}
    try:
        receipt = run(args, dict(os.environ))
        if args.request_only:
            return 0
        print('Upload confirmed; processing VALID. Internal: ' + (receipt['internal_build_state'] or 'unknown')
              + '; external: ' + (receipt['external_build_state'] or 'unknown') + '.')
        print('Groups and public links are in the status receipt; no groups or invitations were changed.')
    except Exception as exc:
        print(str(exc) if isinstance(exc, StatusError) else 'ASC observation failed: ' + type(exc).__name__)
        return 1
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
