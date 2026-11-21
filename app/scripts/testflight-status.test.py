#!/usr/bin/env python3
"""Offline contract checks: stdlib only, with a real disposable OpenSSL key."""
import argparse
import base64
import contextlib
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location('status', Path(__file__).with_name('testflight-status.py'))
status = importlib.util.module_from_spec(spec)
spec.loader.exec_module(status)
beta_spec = importlib.util.spec_from_file_location('beta', Path(__file__).with_name('testflight-beta.py'))
beta = importlib.util.module_from_spec(beta_spec)
beta_spec.loader.exec_module(beta)


def build_page(state='VALID', version='0.2.1', platform='IOS', number='29'):
    item = {'type': 'builds', 'id': 'build-29', 'attributes': {'version': number, 'processingState': state},
            'relationships': {k: {'data': {'type': t, 'id': i}} for k, t, i in (
                ('preReleaseVersion', 'preReleaseVersions', 'pre-21'),
                ('buildBetaDetail', 'buildBetaDetails', 'detail-29'),
                ('betaAppReviewSubmission', 'betaAppReviewSubmissions', 'review-29'))}}
    included = [
        {'type': 'preReleaseVersions', 'id': 'pre-21', 'attributes': {'version': version, 'platform': platform}},
        {'type': 'buildBetaDetails', 'id': 'detail-29', 'attributes': {
            'internalBuildState': 'IN_BETA_TESTING', 'externalBuildState': 'READY_FOR_BETA_SUBMISSION'}},
        {'type': 'betaAppReviewSubmissions', 'id': 'review-29', 'attributes': {'betaReviewState': 'WAITING_FOR_REVIEW'}}]
    return [item], included


def group(gid, internal=False, all_builds=False, enabled=False, link=None):
    return {'type': 'betaGroups', 'id': gid, 'attributes': {
        'name': 'Synthetic ' + gid, 'isInternalGroup': internal, 'hasAccessToAllBuilds': all_builds,
        'publicLinkEnabled': enabled, 'publicLink': link, 'testers': [{'email': 'never-export@example.invalid'}]}}


class StatusTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.env = {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REPOSITORY': 'jt-wang/herdr-anywhere',
                    'GITHUB_REF': 'refs/heads/main', 'TF_DEFAULT_BRANCH': 'main',
                    'GITHUB_SHA': 'a' * 40, 'GITHUB_WORKFLOW_SHA': 'a' * 40,
                    'GITHUB_WORKFLOW_REF': 'jt-wang/herdr-anywhere/.github/workflows/testflight.yml@refs/heads/main',
                    'ASC_KEY_ID': 'SYNTHETIC', 'ASC_ISSUER_ID': '00000000-0000-4000-8000-000000000000',
                    'ASC_PRIVATE_KEY_P8': 'synthetic-private-key', 'RUNNER_TEMP': str(self.root)}
        self.upload = self.root / 'upload'
        self.upload.mkdir()
        self.upload_receipt = {'sha': 'a' * 40, 'version': '0.2.1', 'build': '29', 'uploaded': True,
                               'validated': True, 'unknown-secret': 'never-export-upload-secret'}
        self.write_upload()
        self.args = argparse.Namespace(version='0.2.1', build='29', expected_sha='a' * 40,
                                       status_run_id='', uploaded_sha='', request_only=False,
                                       upload_artifact=str(self.upload), output=str(self.root / 'status.json'), timeout=10)
        self.calls = []
        self.pages = [build_page()]
        self.inventory = build_page()
        self.all_groups, self.assigned = [], []
        self.client = Mock()
        self.client.listing.side_effect = self.listing

    def write_upload(self):
        (self.upload / 'receipt.json').write_text(json.dumps(self.upload_receipt))

    def listing(self, route, params):
        self.calls.append((route, params))
        if route == '/v1/apps':
            self.assertEqual(params['filter[bundleId]'], status.BUNDLE)
            return [{'id': 'app-1', 'attributes': {'bundleId': status.BUNDLE}}], []
        if route == '/v1/builds':
            self.assertEqual(params['filter[app]'], 'app-1')
            if params.get('include') == 'preReleaseVersion':
                self.assertNotIn('filter[version]', params)
                self.assertNotIn('filter[preReleaseVersion.version]', params)
                self.assertNotIn('filter[processingState]', params)
                return self.inventory
            self.assertEqual(params['filter[version]'], '29')
            self.assertEqual(params['filter[preReleaseVersion.version]'], '0.2.1')
            self.assertEqual(params['filter[preReleaseVersion.platform]'], 'IOS')
            return self.pages.pop(0) if len(self.pages) > 1 else self.pages[0]
        self.assertEqual(params, {})
        if route == '/v1/apps/app-1/betaGroups':
            return self.all_groups, []
        match = re.fullmatch('/v1/betaGroups/([A-Za-z0-9-]+)/relationships/builds', route)
        self.assertIsNotNone(match)
        assigned = any(g['id'] == match[1] for g in self.assigned)
        return [{'type': 'builds', 'id': 'build-29' if assigned else 'build-28'}], []

    def run_status(self):
        with patch.object(status, 'Client', return_value=self.client), patch.object(status.time, 'sleep'):
            return status.run(self.args, self.env)

    def cli(self, *extra):
        argv = ['status', '--version', '0.2.1', '--build', '29', '--expected-sha', 'a' * 40,
                '--upload-artifact', str(self.upload), '--output', self.args.output, *extra]
        with patch.dict(os.environ, self.env, clear=True), patch.object(sys, 'argv', argv), \
                patch.object(status, 'Client', return_value=self.client), \
                patch.object(status.subprocess, 'run', side_effect=AssertionError('Must not archive or upload')), \
                contextlib.redirect_stdout(io.StringIO()) as out:
            code = status.main()
        return code, out.getvalue()

    def test_recovery_cli_uses_original_upload_sha_without_rebuilding_or_uploading(self):
        original = 'b' * 40
        self.upload_receipt['sha'] = original
        self.write_upload()
        code, _ = self.cli('--status-run-id', '12345', '--uploaded-sha', original)
        self.assertEqual(code, 0)
        receipt = json.loads(Path(self.args.output).read_text())
        self.assertEqual(receipt['sha'], original)
        self.assertEqual(receipt['observer_sha'], 'a' * 40)
        self.assertEqual(receipt['status_run_id'], '12345')
        self.assertTrue(receipt['uploaded'])

    def test_recovery_cli_rejects_wrong_receipt_sha_and_untrusted_observer_before_asc(self):
        recovery = ('--status-run-id', '12345', '--uploaded-sha', 'b' * 40)
        code, message = self.cli(*recovery)
        self.assertEqual(code, 1)
        self.assertIn('exact upload', message)
        self.assertEqual(self.calls, [])
        self.upload_receipt['sha'] = 'b' * 40
        self.write_upload()
        for key, value in (('GITHUB_SHA', 'b' * 40), ('GITHUB_WORKFLOW_SHA', 'b' * 40),
                           ('GITHUB_REF', 'refs/heads/feature'), ('GITHUB_EVENT_NAME', 'push')):
            with self.subTest(key=key), patch.dict(self.env, {key: value}):
                code, _ = self.cli(*recovery)
                self.assertEqual(code, 1)
                self.assertEqual(self.calls, [])

    def test_request_only_cli_rejects_incomplete_invalid_and_upload_recovery(self):
        for extra in (('--status-run-id', '12345'), ('--uploaded-sha', 'b' * 40),
                      ('--status-run-id', '../123', '--uploaded-sha', 'b' * 40),
                      ('--status-run-id', '0', '--uploaded-sha', 'b' * 40),
                      ('--status-run-id', '12345', '--uploaded-sha', 'short')):
            with self.subTest(extra=extra):
                self.assertEqual(self.cli('--request-only', *extra)[0], 1)
                self.assertEqual(self.calls, [])
        recovery = ('--status-run-id', '12345', '--uploaded-sha', 'b' * 40)
        self.assertEqual(self.cli('--request-only', *recovery)[0], 0)
        with patch.dict(self.env, TF_UPLOAD='true'):
            self.assertEqual(self.cli('--request-only', *recovery)[0], 1)
        self.assertEqual(self.calls, [])
        self.assertFalse(Path(self.args.output).exists())

    def test_exact_build_and_processing_to_valid_without_groups_is_success(self):
        self.pages = [([], []), build_page(version='0.2.0'), build_page('PROCESSING'), build_page()]
        receipt = self.run_status()
        self.assertEqual(sum(route == '/v1/builds' for route, _ in self.calls), 5)
        self.assertTrue(receipt['uploaded'])
        self.assertTrue(receipt['processing_valid'])
        self.assertEqual(receipt['internal_build_state'], 'IN_BETA_TESTING')
        self.assertEqual(receipt['external_build_state'], 'READY_FOR_BETA_SUBMISSION')
        self.assertEqual(receipt['beta_review_state'], 'WAITING_FOR_REVIEW')
        self.assertEqual(receipt['groups'], [])
        self.assertFalse(receipt['public_invitation_for_build'])
        self.assertEqual(receipt['observation'], 'complete')
        self.assertNotIn('unknown-secret', Path(self.args.output).read_text())

    def test_inventory_includes_other_versions_and_failed_builds_and_compares_numerically(self):
        items, included = [], []
        for number, version, state in (('9', '0.1.0', 'VALID'), ('100', '0.2.0', 'INVALID'),
                                       ('30', '0.2.2', 'PROCESSING'), ('29', '0.2.1', 'VALID')):
            rows, related = build_page(number=number, version=version, state=state)
            rows[0]['relationships']['preReleaseVersion']['data']['id'] = 'pre-' + number
            related[0]['id'] = 'pre-' + number
            rows[0]['attributes']['private-field'] = 'never-export-inventory'
            items.extend(rows)
            included.extend(related)
        self.inventory = items, included
        inventory = self.run_status()['build_inventory']
        self.assertEqual(inventory['highest_numeric_build'], 100)
        self.assertTrue(inventory['build_30_occupied'])
        self.assertEqual([(b['version'], b['build']) for b in inventory['builds']],
                         [('0.1.0', '9'), ('0.2.0', '100'), ('0.2.2', '30'), ('0.2.1', '29')])
        self.assertRegex(inventory['queried_at'], r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$')
        self.assertNotIn('never-export-inventory', Path(self.args.output).read_text())
        self.inventory = build_page()
        self.assertFalse(self.run_status()['build_inventory']['build_30_occupied'])

    def test_wrong_version_platform_number_and_app_never_match(self):
        for kwargs in ({'version': '0.2.0'}, {'platform': 'MAC_OS'}, {'number': '28'}):
            with self.subTest(kwargs=kwargs):
                items, included = build_page(**kwargs)
                self.assertIsNone(status.exact_build(items, included, 'app-1', '0.2.1', '29'))
        items, included = build_page()
        items[0]['relationships']['app'] = {'data': {'id': 'other-app'}}
        self.assertIsNone(status.exact_build(items, included, 'app-1', '0.2.1', '29'))
        items, included = build_page()
        with self.assertRaisesRegex(status.StatusError, 'Multiple exact'):
            status.exact_build(items * 2, included, 'app-1', '0.2.1', '29')

    def test_failed_invalid_unknown_and_timeout_keep_upload_distinct(self):
        for state in ('FAILED', 'INVALID', 'UNKNOWN'):
            with self.subTest(state=state):
                self.pages = [build_page(state)]
                with self.assertRaisesRegex(status.StatusError, 'processing failed'):
                    self.run_status()
                saved = json.loads(Path(self.args.output).read_text())
                self.assertTrue(saved['uploaded'])
                self.assertFalse(saved['processing_valid'])
                self.assertEqual(saved['processing_state'], state)
        self.pages = [build_page('PROCESSING')]
        clock = iter([0, 0, 11, 11])
        with patch.object(status.time, 'monotonic', side_effect=lambda: next(clock)):
            with self.assertRaisesRegex(status.StatusError, 'timed out'):
                self.run_status()
        saved = json.loads(Path(self.args.output).read_text())
        self.assertTrue(saved['uploaded'])
        self.assertEqual(saved['processing_state'], 'PROCESSING')
        self.assertEqual(saved['observation'], 'error')

    def test_groups_distinguish_app_only_build_assignment_and_disabled_link(self):
        link = 'https://testflight.apple.com/join/Synthetic42'
        self.all_groups = [group('automatic', internal=True, all_builds=True),
                           group('other-build', enabled=True, link=link),
                           group('disabled', enabled=False, link=link),
                           group('public', enabled=True, link=link)]
        self.assigned = [copy.deepcopy(self.all_groups[i]) for i in (2, 3)]
        receipt = self.run_status()
        rows = {g['id']: g for g in receipt['groups']}
        self.assertTrue(rows['automatic']['has_build_access'])
        self.assertFalse(rows['automatic']['assigned_to_build'])
        self.assertFalse(rows['other-build']['has_build_access'])
        self.assertFalse(rows['other-build']['public_invitation_for_build'])
        self.assertEqual(rows['other-build']['public_link'], link)
        self.assertFalse(rows['disabled']['public_invitation_for_build'])
        self.assertTrue(rows['public']['public_invitation_for_build'])
        self.assertTrue(receipt['public_invitation_for_build'])
        self.assertNotIn('never-export', Path(self.args.output).read_text())

    def test_upload_proof_and_trust_guard_precede_any_asc_call(self):
        for key, value in (('uploaded', False), ('validated', False), ('build', '28'), ('sha', 'b' * 40)):
            with self.subTest(key=key), patch.object(status, 'Client') as client:
                self.upload_receipt[key] = value
                self.write_upload()
                with self.assertRaisesRegex(status.StatusError, 'exact upload'):
                    status.run(self.args, self.env)
                client.assert_not_called()
                self.upload_receipt.update(sha='a' * 40, build='29', uploaded=True, validated=True)
        self.write_upload()
        self.env['GITHUB_SHA'] = 'b' * 40
        with patch.object(status, 'Client') as client:
            with self.assertRaisesRegex(Exception, 'Trusted'):
                status.run(self.args, self.env)
            client.assert_not_called()

    def test_http_errors_never_echo_body_token_or_url_credentials(self):
        client = status.Client(self.env, time.monotonic() + 10)
        for code in (401, 403, 429, 500):
            client.opener.open = Mock(side_effect=urllib.error.HTTPError(
                status.ORIGIN + '/v1/builds?private=never-export', code,
                'never-export-private-error', {'Authorization': 'never-export-token'}, io.BytesIO(b'never-export-body')))
            with patch.object(status, 'token', return_value='never-export-token'):
                with self.assertRaises(status.StatusError) as error:
                    client.get(status.ORIGIN + '/v1/builds')
            self.assertEqual(str(error.exception), 'ASC HTTP ' + str(code) + ' on /v1/builds')
        for failure in (urllib.error.URLError('never-export'), TimeoutError('never-export')):
            client.opener.open = Mock(side_effect=failure)
            with patch.object(status, 'token', return_value='never-export-token'):
                with self.assertRaisesRegex(status.StatusError, '^ASC network error or request timeout$'):
                    client.get(status.ORIGIN + '/v1/builds')
        self.client.listing.side_effect = status.StatusError('ASC HTTP 403 on /v1/apps')
        with self.assertRaisesRegex(status.StatusError, 'HTTP 403'):
            self.run_status()
        self.assertEqual(json.loads(Path(self.args.output).read_text())['error'], 'ASC HTTP 403 on /v1/apps')

    def test_http_error_only_exact_allowlisted_machine_codes_reach_console_and_receipt(self):
        bodies = [
            ({'errors': [{'code': 'FORBIDDEN_ERROR', 'detail': 'never-export-detail',
                          'title': 'never-export-title', 'id': 'never-export-id'},
                         {'code': 'NOT_AUTHORIZED'}, {'code': 'FORBIDDEN_ERROR'},
                         {'code': 'FORBIDDEN_ERROR.never-export'}, {'code': 'NEVER_EXPORT'},
                         {'code': ['FORBIDDEN_ERROR']}, None]},
             ' (codes: FORBIDDEN_ERROR, NOT_AUTHORIZED)'),
            ({'errors': [{'code': 'AUTHENTICATION_ERROR'}]}, ' (codes: AUTHENTICATION_ERROR)'),
            ({'errors': [{'code': 'PARAMETER_ERROR.INVALID', 'source': {'parameter': 'filter[builds]'},
                          'detail': 'never-export-detail'},
                         {'code': 'PARAMETER_ERROR.INVALID.never-export',
                          'source': {'parameter': 'never-export-parameter'}}]},
             ' (codes: PARAMETER_ERROR.INVALID) (parameters: filter[builds])'),
            ({'errors': [{'code': 'UNRECOGNIZED_UPPERCASE_SECRET', 'detail': 'FORBIDDEN_ERROR'}]}, ''),
            ({'errors': 'FORBIDDEN_ERROR'}, ''), ([], ''), ({'errors': [42, {}]}, '')]
        for body, suffix in bodies:
            with self.subTest(body=body):
                client = status.Client(self.env, time.monotonic() + 10)
                client.opener.open = Mock(side_effect=urllib.error.HTTPError(
                    status.ORIGIN, 403, 'never-export-message', {}, io.BytesIO(json.dumps(body).encode())))
                self.client.listing.side_effect = lambda route, params: client.get(
                    status.ORIGIN + route + '?private=never-export-query')
                with patch.object(status, 'token', return_value='never-export-token'):
                    code, message = self.cli()
                expected = 'ASC HTTP 403 on /v1/apps' + suffix
                self.assertEqual(code, 1)
                self.assertEqual(message.strip(), expected)
                self.assertEqual(json.loads(Path(self.args.output).read_text())['error'], expected)
        for body in (b'not-json-never-export', b'\xff', b' ' * 65537,
                     b'{"errors":[{"code":"FORBIDDEN_ERROR"}]}' + b' ' * 65537):
            client.opener.open = Mock(side_effect=urllib.error.HTTPError(
                status.ORIGIN, 403, 'never-export', {}, io.BytesIO(body)))
            with patch.object(status, 'token', return_value='never-export-token'):
                with self.assertRaisesRegex(status.StatusError, '^ASC HTTP 403 on /v1/apps$'):
                    client.get(status.ORIGIN + '/v1/apps')

    def test_real_client_scopes_each_request_including_encoded_query_and_pagination(self):
        generated = subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256'],
                                   capture_output=True, check=True)
        self.env['ASC_PRIVATE_KEY_P8'] = generated.stdout.decode()
        client = status.Client(self.env, time.monotonic() + 10)
        items, included = build_page()
        inventory_items, inventory_included = build_page(number='30', version='0.2.2', state='PROCESSING')
        next_url = status.ORIGIN + '/v1/apps?filter%5BbundleId%5D=' + status.BUNDLE + '&fields%5Bapps%5D=bundleId&cursor=a%2Bb%26c'
        inventory_next = status.ORIGIN + '/v1/builds?filter%5Bapp%5D=app-1&include=preReleaseVersion&cursor=inventory2'
        pages = [{'data': [], 'links': {'next': next_url}},
                 {'data': [{'id': 'app-1', 'attributes': {'bundleId': status.BUNDLE}}]},
                 {'data': items, 'included': included}, {'data': [group('group-1')]},
                 {'data': [{'type': 'builds', 'id': 'build-29'}]},
                 {'data': [], 'links': {'next': inventory_next}},
                 {'data': inventory_items, 'included': inventory_included}]
        client.opener.open = Mock(side_effect=[contextlib.closing(io.BytesIO(json.dumps(p).encode())) for p in pages])
        receipt = {}
        status.observe(client, '0.2.1', '29', receipt, time.monotonic() + 10)
        self.assertEqual(receipt['build_inventory']['highest_numeric_build'], 30)
        self.assertTrue(receipt['build_inventory']['build_30_occupied'])
        targets = []
        for call in client.opener.open.call_args_list:
            request = call.args[0]
            self.assertEqual(request.get_method(), 'GET')
            jwt = request.get_header('Authorization').removeprefix('Bearer ')
            payload = jwt.split('.')[1]
            claims = json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))
            target = request.full_url.removeprefix(status.ORIGIN)
            self.assertEqual(claims['scope'], ['GET ' + target])
            targets.append(target)
        self.assertEqual(len(set(targets)), 7)
        self.assertIn('cursor=a%2Bb%26c', targets[1])
        self.assertIn('fields%5Bapps%5D=', targets[0])
        self.assertIn('filter%5BpreReleaseVersion.version%5D=', targets[2])
        self.assertEqual(targets[3], '/v1/apps/app-1/betaGroups')
        self.assertEqual(targets[4], '/v1/betaGroups/group-1/relationships/builds')
        self.assertEqual(targets[6], inventory_next.removeprefix(status.ORIGIN))

    def test_pagination_is_read_only_same_origin_and_redirects_are_rejected(self):
        client = status.Client(self.env, time.monotonic() + 10)
        client.opener.open = Mock(side_effect=AssertionError('Unexpected network request'))
        next_url = status.ORIGIN + '/v1/builds?cursor=2'
        pages = [{'data': [{'id': '1'}], 'links': {'next': next_url}}, {'data': [{'id': '2'}]}]
        with patch.object(client, 'get', side_effect=pages):
            data, _ = client.listing('/v1/builds', {})
        self.assertEqual([v['id'] for v in data], ['1', '2'])
        for url in ('https://example.invalid/v1/builds', 'http://api.appstoreconnect.apple.com/v1/builds',
                    'https://api.appstoreconnect.apple.com@evil.invalid/v1/builds',
                    status.ORIGIN + '/v1/betaTesters', status.ORIGIN + ':443/v1/builds',
                    status.ORIGIN + '/v1/apps/app-1/betaTesters',
                    status.ORIGIN + '/v1/betaGroups/group-1/relationships/betaTesters',
                    status.ORIGIN + '/v1/betaGroups/../relationships/builds'):
            with self.subTest(url=url), patch.object(status, 'token') as token:
                with self.assertRaisesRegex(status.StatusError, 'Unsafe'):
                    client.get(url)
                token.assert_not_called()
            with patch.object(client, 'get', return_value={'data': [], 'links': {'next': url}}) as get:
                with self.assertRaisesRegex(status.StatusError, 'Unsafe'):
                    client.listing('/v1/builds', {})
                self.assertEqual(get.call_count, 1)
        with self.assertRaisesRegex(status.StatusError, 'redirect rejected'):
            status.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://example.invalid')
        with patch.object(status, 'token', return_value='synthetic-token'):
            client.opener.open = Mock(return_value=contextlib.closing(io.BytesIO(b'{"data":[]}')))
            client.get(status.ORIGIN + '/v1/builds')
        self.assertEqual(client.opener.open.call_args.args[0].get_method(), 'GET')

    def test_real_es256_jwt_and_signing_failure_clean_private_files_and_environment(self):
        generated = subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256'],
                                   capture_output=True, check=True)
        self.env['ASC_PRIVATE_KEY_P8'] = generated.stdout.decode()
        self.env.update(APPLE_TEAM_ID='private-team', IOS_DISTRIBUTION_P12_PASSWORD='private-password',
                        API_PRIVATE_KEYS_DIR='private-path')
        external = subprocess.run
        seen = []
        def signing(args, **kwargs):
            key = Path(args[-1])
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
            self.assertEqual(key.parent.stat().st_mode & 0o777, 0o700)
            self.assertEqual(key.read_text(), self.env['ASC_PRIVATE_KEY_P8'])
            self.assertFalse(any(k.startswith(('ASC_', 'IOS_')) or k in ('APPLE_TEAM_ID', 'API_PRIVATE_KEYS_DIR')
                                 for k in kwargs['env']))
            self.assertNotIn('PRIVATE KEY', ' '.join(args))
            seen.append(key)
            return external(args, **kwargs)
        with patch.object(status.subprocess, 'run', side_effect=signing):
            jwt = status.token(self.env, status.ORIGIN + '/v1/apps?filter%5BbundleId%5D=synthetic')
        header, payload, signature = [base64.urlsafe_b64decode(p + '=' * (-len(p) % 4)) for p in jwt.split('.')]
        self.assertEqual(json.loads(header), {'alg': 'ES256', 'kid': 'SYNTHETIC', 'typ': 'JWT'})
        claims = json.loads(payload)
        self.assertEqual(claims['aud'], 'appstoreconnect-v1')
        self.assertEqual(claims['exp'] - claims['iat'], 120)
        self.assertEqual(claims['scope'], ['GET /v1/apps?filter%5BbundleId%5D=synthetic'])
        self.assertEqual(len(signature), 64)
        # Independently verify the real signature after converting JOSE R||S back to DER.
        integers = []
        for part in (signature[:32], signature[32:]):
            value = part.lstrip(b'\0') or b'\0'
            if value[0] & 0x80:
                value = b'\0' + value
            integers.append(bytes([2, len(value)]) + value)
        contents = b''.join(integers)
        sigfile, pubfile = self.root / 'signature.der', self.root / 'public.pem'
        sigfile.write_bytes(bytes([0x30, len(contents)]) + contents)
        public = external(['openssl', 'pkey', '-pubout'], input=generated.stdout, capture_output=True, check=True)
        pubfile.write_bytes(public.stdout)
        verified = external(['openssl', 'dgst', '-sha256', '-verify', str(pubfile), '-signature', str(sigfile)],
                            input='.'.join(jwt.split('.')[:2]).encode(), capture_output=True)
        self.assertEqual(verified.returncode, 0)
        self.assertTrue(all(not key.exists() for key in seen))
        argv = ['status', '--version', '0.2.1', '--build', '29', '--expected-sha', 'a' * 40,
                '--upload-artifact', str(self.upload), '--output', self.args.output]
        failure = subprocess.CompletedProcess([], 17, b'never-export-secret', b'never-export-private-stderr')
        with patch.dict(os.environ, self.env, clear=True), patch.object(sys, 'argv', argv), \
                patch.object(status.subprocess, 'run', return_value=failure), contextlib.redirect_stdout(io.StringIO()) as out:
            self.assertEqual(status.main(), 1)
        self.assertIn('openssl exit 17', out.getvalue())
        self.assertNotIn('never-export', out.getvalue() + Path(self.args.output).read_text())
        self.assertFalse(list(self.root.glob('asc-status-*')))
        with patch.object(status.subprocess, 'run', side_effect=TimeoutError('never-export')):
            with self.assertRaises(TimeoutError):
                status.token(self.env, status.ORIGIN + '/v1/apps')
        self.assertFalse(list(self.root.glob('asc-status-*')))

    def test_workflow_status_is_separate_gated_and_has_only_asc_secrets(self):
        workflow = (Path(__file__).resolve().parents[2] / '.github/workflows/testflight.yml').read_text()
        request, rest = workflow.split('\n  archive:', 1)
        self.assertIn('python3 app/scripts/testflight-status.test.py', request)
        self.assertIn('python3 app/scripts/testflight.py --version', request)
        self.assertIn('--uploaded-sha "$TF_UPLOADED_SHA" --request-only', request)
        _, job = rest.split('\n  status:', 1)
        self.assertIn('needs: [request, archive]', job)
        self.assertIn("needs.request.result == 'success'", job)
        self.assertIn("inputs.upload && needs.archive.result == 'success'", job)
        self.assertIn('environment: testflight', job)
        self.assertEqual(set(re.findall(r'secrets\.([A-Z0-9_]+)', job)),
                         {'ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8'})
        self.assertNotIn('scripts/testflight.py ', job)
        self.assertIn('overwrite: true', job)
        self.assertIn('if: always()', job)

    def test_workflow_recovery_skips_archive_and_selects_only_the_explicit_old_run(self):
        workflow = (Path(__file__).resolve().parents[2] / '.github/workflows/testflight.yml').read_text()
        archive, job = workflow.split('\n  archive:', 1)[1].split('\n  status:', 1)
        archive_if = re.search(r'^    if: (.+)$', archive, re.M)[1]
        status_if = re.search(r'^    if: (.+)$', job, re.M)[1]
        # Evaluate these boolean workflow guards over a dispatch/outcome matrix.
        beta_if = re.search(r'^    if: (.+)$', workflow.split('\n  beta:', 1)[1], re.M)[1]
        def enabled(expression, run_id='', upload=False, request='success', result='skipped', cancelled=False,
                    submit=False):
            values = {'inputs.status_run_id': run_id, 'inputs.upload': upload, 'inputs.submit_beta': submit,
                      'needs.request.result': request, 'needs.archive.result': result,
                      'github.repository': 'jt-wang/herdr-anywhere', 'github.ref': 'refs/heads/main',
                      'github.event.repository.default_branch': 'main',
                      'github.workflow_sha': 'a' * 40, 'github.sha': 'a' * 40,
                      'always()': True, 'cancelled()': cancelled}
            for key in sorted(values, key=len, reverse=True):
                expression = expression.replace(key, repr(values[key]))
            expression = expression.replace('&&', ' and ').replace('||', ' or ')
            expression = re.sub(r'!(?!=)', 'not ', expression)
            return eval(expression, {'__builtins__': {}, 'format': lambda s, value: s.format(value)})

        for upload in (False, True):
            self.assertFalse(enabled(archive_if, run_id='12345', upload=upload))
            self.assertTrue(enabled(archive_if, upload=upload))
            self.assertFalse(enabled(archive_if, upload=upload, submit=True))
            self.assertFalse(enabled(beta_if, upload=upload))
            self.assertFalse(enabled(beta_if, run_id='12345', upload=upload, submit=True))
        self.assertTrue(enabled(beta_if, submit=True))
        self.assertFalse(enabled(beta_if, upload=True, submit=True))
        self.assertFalse(enabled(status_if, submit=True))
        self.assertTrue(enabled(status_if, run_id='12345'))
        self.assertTrue(enabled(status_if, upload=True, result='success'))
        self.assertFalse(enabled(status_if, result='success'))
        self.assertFalse(enabled(status_if, upload=True, result='failure'))
        self.assertFalse(enabled(status_if, upload=True, result='skipped'))
        for result in ('success', 'failure', 'cancelled'):
            self.assertFalse(enabled(status_if, run_id='12345', result=result))
        for request in ('failure', 'skipped', 'cancelled'):
            self.assertFalse(enabled(status_if, run_id='12345', request=request))
        self.assertFalse(enabled(status_if, run_id='12345', cancelled=True))
        downloads = re.findall(r'      - uses: actions/download-artifact@[^\n]+\n(.*?)(?=      - )', job, re.S)
        self.assertEqual(len(downloads), 2)
        for run_id in ('', '12345'):
            selected = [step for step in downloads if enabled(re.search(r'        if: (.+)', step)[1], run_id=run_id)]
            self.assertEqual(len(selected), 1)
            self.assertEqual('run-id:' in selected[0], bool(run_id))
            self.assertEqual('github-token:' in selected[0], bool(run_id))
            self.assertIn('name: testflight-${{ inputs.build }}', selected[0])
        self.assertIn('run-id: ${{ inputs.status_run_id }}', downloads[1])
        self.assertIn('actions: read', job)
        self.assertIn('ref: ${{ github.sha }}', job)
        self.assertNotIn('ref: ${{ inputs.uploaded_sha }}', workflow)
        self.assertIn('--status-run-id "$TF_STATUS_RUN_ID" --uploaded-sha "$TF_UPLOADED_SHA"', job)

    def test_beta_submits_only_a_ready_unreviewed_build_and_leaves_reviewed_builds_alone(self):
        public = group('group-public', enabled=True, link='https://testflight.apple.com/join/abc')
        public['attributes']['name'] = 'Public'
        self.all_groups = [group('group-internal', internal=True, all_builds=True), public]
        receipt = {}
        def page(external, review):
            items, included = build_page()
            included[1]['attributes']['externalBuildState'] = external
            if review is None:
                del items[0]['relationships']['betaAppReviewSubmission']
            else:
                included[2]['attributes']['betaReviewState'] = review
            return items, included
        # Like builds 38 and 39: already in Public with a review, so nothing is written.
        for external, review in (('IN_BETA_TESTING', 'APPROVED'), ('WAITING_FOR_BETA_REVIEW', 'WAITING_FOR_REVIEW'),
                                 ('BETA_REJECTED', 'REJECTED')):
            with self.subTest(review=review):
                self.pages, self.assigned = [page(external, review)], [public]
                beta.submit(self.client, '0.2.1', '29', receipt, time.monotonic() + 10)
                self.client.send.assert_not_called()
        self.pages, self.assigned = [page('MISSING_EXPORT_COMPLIANCE', None)], []
        with self.assertRaisesRegex(beta.status.StatusError, 'MISSING_EXPORT_COMPLIANCE, not READY_FOR_BETA_SUBMISSION'):
            beta.submit(self.client, '0.2.1', '29', {}, time.monotonic() + 10)
        self.client.send.assert_not_called()
        self.pages = [page('READY_FOR_BETA_SUBMISSION', None), page('WAITING_FOR_BETA_REVIEW', 'WAITING_FOR_REVIEW')]
        receipt = {}
        beta.submit(self.client, '0.2.1', '29', receipt, time.monotonic() + 10)
        ref = {'type': 'builds', 'id': 'build-29'}
        self.assertEqual(self.client.send.call_args_list, [
            unittest.mock.call('POST', status.ORIGIN + '/v1/betaGroups/group-public/relationships/builds', {'data': [ref]}),
            unittest.mock.call('POST', status.ORIGIN + '/v1/betaAppReviewSubmissions', {'data': {
                'type': 'betaAppReviewSubmissions', 'relationships': {'build': {'data': ref}}}})])
        self.assertEqual((receipt['added_to_group'], receipt['submitted'], receipt['beta_review_state']),
                         (True, True, 'WAITING_FOR_REVIEW'))

    def test_writes_are_unscoped_short_lived_posts_to_two_routes_only(self):
        generated = subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256'],
                                   capture_output=True, check=True)
        self.env['ASC_PRIVATE_KEY_P8'] = generated.stdout.decode()
        client = status.Client(self.env, time.monotonic() + 10)
        client.opener.open = Mock(side_effect=[contextlib.closing(io.BytesIO(b'')),
                                               contextlib.closing(io.BytesIO(b'{"data":{"id":"review-1"}}'))])
        self.assertIsNone(client.send('POST', status.ORIGIN + '/v1/betaGroups/group-1/relationships/builds', {'data': []}))
        self.assertEqual(client.send('POST', status.ORIGIN + '/v1/betaAppReviewSubmissions', {'data': {}}),
                         {'data': {'id': 'review-1'}})
        for call, path in zip(client.opener.open.call_args_list,
                              ('/v1/betaGroups/group-1/relationships/builds', '/v1/betaAppReviewSubmissions')):
            request = call.args[0]
            payload = request.get_header('Authorization').removeprefix('Bearer ').split('.')[1]
            claims = json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))
            # App Store Connect takes only GET in a token's scope (a POST-scoped token gets 405), so writes carry none.
            self.assertEqual(request.get_method(), 'POST')
            self.assertNotIn('scope', claims)
            self.assertEqual(claims['exp'] - claims['iat'], 120)
            self.assertEqual(request.get_header('Content-type'), 'application/json')
        for method, url in (('POST', status.ORIGIN + '/v1/appStoreVersionSubmissions'),
                            ('POST', status.ORIGIN + '/v1/reviewSubmissions'),
                            ('POST', status.ORIGIN + '/v1/betaGroups/group-1/relationships/betaTesters'),
                            ('POST', status.ORIGIN + '/v1/betaAppReviewSubmissions?x=1'),
                            ('POST', 'https://example.invalid/v1/betaAppReviewSubmissions'),
                            ('DELETE', status.ORIGIN + '/v1/betaGroups/group-1/relationships/builds'),
                            ('PATCH', status.ORIGIN + '/v1/builds')):
            with self.subTest(method=method, url=url), patch.object(status, 'token') as token:
                with self.assertRaisesRegex(status.StatusError, 'Unsafe'):
                    client.send(method, url, {})
                token.assert_not_called()


if __name__ == '__main__':
    unittest.main()
