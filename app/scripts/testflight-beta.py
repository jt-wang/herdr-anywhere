#!/usr/bin/env python3
"""Add one exact, processed iOS build to the Public TestFlight group and submit it for Beta App Review.

Never submits for App Store review and never changes another build or group. A build that already has a
beta review (waiting, in review, approved or rejected) is only read back, so a repeat run changes nothing.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import time

spec = importlib.util.spec_from_file_location('status', Path(__file__).with_name('testflight-status.py'))
status = importlib.util.module_from_spec(spec)
spec.loader.exec_module(status)

GROUP = 'Public'


def submit(client, version, build, receipt, deadline):
    status.observe(client, version, build, receipt, deadline)
    public = [g for g in receipt['groups'] if g['name'] == GROUP and not g['is_internal']]
    if len(public) != 1:
        raise status.StatusError('Expected exactly one external group named ' + GROUP)
    if receipt['beta_review_state'] is None:
        if receipt['external_build_state'] != 'READY_FOR_BETA_SUBMISSION':
            raise status.StatusError('Build is ' + str(receipt['external_build_state'])
                                     + ', not READY_FOR_BETA_SUBMISSION; nothing changed')
        build_ref = {'type': 'builds', 'id': receipt['build_id']}
        if not public[0]['assigned_to_build']:
            client.send('POST', status.ORIGIN + '/v1/betaGroups/' + public[0]['id'] + '/relationships/builds',
                        {'data': [build_ref]})
            receipt['added_to_group'] = True
        client.send('POST', status.ORIGIN + '/v1/betaAppReviewSubmissions',
                    {'data': {'type': 'betaAppReviewSubmissions', 'relationships': {'build': {'data': build_ref}}}})
        receipt['submitted'] = True
    # Read back what App Store Connect now reports.
    status.observe(client, version, build, receipt, deadline)


def run(args, env):
    spec = importlib.util.spec_from_file_location('testflight', Path(__file__).with_name('testflight.py'))
    release = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(release)
    release.release(Path(__file__).resolve().parents[2], args.version, args.build, args.expected_sha,
                    env=env, request_only=True)
    if env.get('TF_UPLOAD') == 'true' or env.get('TF_STATUS_RUN_ID'):
        raise status.StatusError('Beta submission runs alone: leave upload false and status_run_id empty')
    receipt = {'observer_sha': args.expected_sha, 'bundle_id': status.BUNDLE, 'version': args.version,
               'build': args.build, 'group': GROUP, 'added_to_group': False, 'submitted': False,
               'observation': 'error'}
    try:
        missing = [k for k in ('ASC_KEY_ID', 'ASC_ISSUER_ID', 'ASC_PRIVATE_KEY_P8') if not env.get(k)]
        if missing:
            raise status.StatusError('Missing ASC configuration: ' + ', '.join(missing))
        deadline = time.monotonic() + 300
        submit(status.Client(env, deadline), args.version, args.build, receipt, deadline)
        receipt['observation'] = 'complete'
    except status.StatusError as exc:
        receipt['error'] = str(exc)
        raise
    except Exception as exc:
        receipt['error'] = 'ASC beta submission failed: ' + type(exc).__name__
        raise status.StatusError(receipt['error']) from None
    finally:
        receipt.pop('build_inventory', None)
        Path(args.output).write_text(json.dumps(receipt, indent=2) + '\n')
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--build', required=True)
    parser.add_argument('--expected-sha', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        receipt = run(args, dict(os.environ))
    except Exception as exc:
        print(str(exc) if isinstance(exc, status.StatusError) else 'ASC beta submission failed: ' + type(exc).__name__)
        return 1
    print('Added to ' + GROUP + ': ' + str(receipt['added_to_group']) + '; submitted: ' + str(receipt['submitted'])
          + '; beta review: ' + str(receipt['beta_review_state']) + '; external: ' + str(receipt['external_build_state']) + '.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
