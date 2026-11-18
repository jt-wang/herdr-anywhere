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
                                       upload_artifact=str(self.upload), output=str(self.root / 'status.json'), timeout=10)
        self.calls = []
        self.pages = [build_page()]
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
            self.assertEqual(params['filter[version]'], '29')
            self.assertEqual(params['filter[preReleaseVersion.version]'], '0.2.1')
            self.assertEqual(params['filter[preReleaseVersion.platform]'], 'IOS')
            return self.pages.pop(0) if len(self.pages) > 1 else self.pages[0]
        self.assertEqual(route, '/v1/betaGroups')
        self.assertEqual(params['filter[app]'], 'app-1')
        if 'filter[builds]' in params:
            self.assertEqual(params['filter[builds]'], 'build-29')
            return self.assigned, []
        return self.all_groups, []

    def run_status(self):
        with patch.object(status, 'Client', return_value=self.client), patch.object(status.time, 'sleep'):
            return status.run(self.args, self.env)

    def test_exact_build_and_processing_to_valid_without_groups_is_success(self):
        self.pages = [([], []), build_page(version='0.2.0'), build_page('PROCESSING'), build_page()]
        receipt = self.run_status()
        self.assertEqual(sum(route == '/v1/builds' for route, _ in self.calls), 4)
        self.assertTrue(receipt['uploaded'])
        self.assertTrue(receipt['processing_valid'])
        self.assertEqual(receipt['internal_build_state'], 'IN_BETA_TESTING')
        self.assertEqual(receipt['external_build_state'], 'READY_FOR_BETA_SUBMISSION')
        self.assertEqual(receipt['beta_review_state'], 'WAITING_FOR_REVIEW')
        self.assertEqual(receipt['groups'], [])
        self.assertFalse(receipt['public_invitation_for_build'])
        self.assertEqual(receipt['observation'], 'complete')
        self.assertNotIn('unknown-secret', Path(self.args.output).read_text())

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
                    status.ORIGIN + '/v1/betaTesters', status.ORIGIN + ':443/v1/builds'):
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
            jwt = status.token(self.env)
        header, payload, signature = [base64.urlsafe_b64decode(p + '=' * (-len(p) % 4)) for p in jwt.split('.')]
        self.assertEqual(json.loads(header), {'alg': 'ES256', 'kid': 'SYNTHETIC', 'typ': 'JWT'})
        claims = json.loads(payload)
        self.assertEqual(claims['aud'], 'appstoreconnect-v1')
        self.assertEqual(claims['exp'] - claims['iat'], 120)
        self.assertTrue(all(s.startswith('GET ') for s in claims['scope']))
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
                status.token(self.env)
        self.assertFalse(list(self.root.glob('asc-status-*')))

    def test_workflow_status_is_separate_gated_and_has_only_asc_secrets(self):
        workflow = (Path(__file__).resolve().parents[2] / '.github/workflows/testflight.yml').read_text()
        request, rest = workflow.split('\n  archive:', 1)
        self.assertIn('python3 app/scripts/testflight-status.test.py', request)
        _, job = rest.split('\n  status:', 1)
        self.assertIn('needs: archive', job)
        self.assertIn("if: inputs.upload && needs.archive.result == 'success'", job)
        self.assertIn('environment: testflight', job)
        import re
        self.assertEqual(set(re.findall(r'secrets\.([A-Z0-9_]+)', job)),
                         {'ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8'})
        self.assertNotIn('scripts/testflight.py ', job)
        self.assertIn('overwrite: true', job)
        self.assertIn('if: always()', job)


if __name__ == '__main__':
    unittest.main()
