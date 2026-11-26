#!/usr/bin/env python3
"""Whether one public commit may be released: on that very commit, the newest push run of Tests, Computers and
History has finished and passed (a push runs them whole; a dispatch may run only part), and so has the newest
release run of Devices: the nightly run (event schedule, title "Devices nightly") or a dispatch of groups=release and
nothing else (event workflow_dispatch, title "Devices groups release"; devices.yml's run-name adds every input that
changes what runs). In it, each release group's own job passed (app/e2e/ci/groups.sh release, its job names read
from devices.yml), every other job in it passed or was skipped by its condition, and no other Devices run on the
commit failed. One exception, the known issue in CHANGELOG.md: the left-edge back swipe job may fail in its Suites
step, and nowhere else, when its artifact shows that each of the 288 swipes ran once and 1 to 3 of them didn't go
back, each with the page it started on still shown and not going back late either; the gate says so. It reads
GitHub's API through `gh`, and says each problem.

Usage: python3 -I app/e2e/ci/release-gate.py SHA [--repo OWNER/NAME]    exit 0: no problems
       python3 -I app/e2e/ci/release-gate.py --self-test
"""
import json
import os
import re
import subprocess
import sys
import tempfile

REPO = 'jt-wang/herdr-anywhere'
EVENTS = ('push', 'workflow_dispatch', 'schedule')
HERE = os.path.dirname(os.path.abspath(__file__))
GROUPS = os.path.join(HERE, 'groups.sh')
DEVICES = os.path.join(HERE, '..', '..', '..', '.github', 'workflows', 'devices.yml')
DEVICES_PATH = '.github/workflows/devices.yml'
# The known issue in CHANGELOG.md (v0.3.12): a swipe back from the left edge on iPhone that now and then doesn't start.
# Its job, artifact and most misses (the worst seen is 3), and the swipes ios-edge-back.sh makes: each page, height,
# distance from the edge and speed 6 times (EdgeBack.swift's grid, the script's N=6).
EDGE_JOB, EDGE_ARTIFACT, EDGE_MAX_MISS = 'iOS swipe back from the left edge', 'ios-edge', 3
EDGE_CASES = frozenset(f'{p}-{y}-{x}-{v}-{i}' for p in ('space', 'space-asking', 'settings', 'space-settings')
                       for y in ('header', 'middle', 'low') for x in (2, 12) for v in ('slow', 'fast') for i in range(1, 7))
# The job's steps that run after Suites whatever it did (devices.yml: if: always()), by name or name prefix.
EDGE_AFTER = ('Check what is uploaded', 'Shut the simulator', 'Run actions/upload-artifact@')
# The jobs a group that is not a matrix key runs, by groups.sh's output that turns it on.
FLAG_JOBS = {'update': ["Build the update test's files", 'Android update, upgrade from the previous release'],
             'remote': ['A picture on another machine (docker)'],
             'user_path': ['Install steps from the release files (Mac)']}


def gh(path):
    out = subprocess.run(['gh', 'api', path], capture_output=True, text=True)
    if out.returncode != 0:
        raise SystemExit(f'gh api {path}: {out.stderr.strip()}')
    return json.loads(out.stdout)


def expected_jobs(yml=None):
    """The name of each job a release run must pass: one per matrix key groups.sh gives, as devices.yml names the
    iOS and Android jobs, and the jobs of update, remote and user_path."""
    yml = yml if yml is not None else open(DEVICES).read()
    out = subprocess.run(['sh', GROUPS, 'release', '0', '0'], capture_output=True, text=True, check=True).stdout
    keys = dict(line.split('=', 1) for line in out.splitlines())
    names = []
    for os_name in ('iOS', 'Android'):
        m = re.search(r"^    name: " + os_name + r" \$\{\{ fromJSON\('(\{.*?\})'\)\[matrix\.key\]", yml, re.M)
        if not m:
            raise SystemExit(f'devices.yml has no {os_name} job named by matrix key')
        named = json.loads(m.group(1))
        names += [f'{os_name} {named[k]}' for k in json.loads(keys['ios' if os_name == 'iOS' else 'android'])]
    for flag, jobs in FLAG_JOBS.items():
        for n in jobs:
            if f'\n    name: {n}\n' not in yml:
                raise SystemExit(f'devices.yml has no job named {n}')
        if keys[flag] == 'true':
            names += jobs
    return names


def is_release(run):
    """A run of the release groups as they are, with no input that changes what runs."""
    return ((run.get('event'), run.get('display_title')) in
            (('schedule', 'Devices nightly'), ('workflow_dispatch', 'Devices groups release')))


def known_edge_job(job):
    """Whether the left-edge job failed in its Suites step and nowhere else: Suites the only step that neither passed
    nor was skipped, and the steps that run after it whatever it did each passed."""
    steps = job.get('steps') or []
    odd = [x for x in steps if x.get('conclusion') not in ('success', 'skipped')]
    if job.get('conclusion') != 'failure' or [(x['name'], x['conclusion']) for x in odd] != [('Suites', 'failure')]:
        return False
    after = [x for x in steps if x['number'] > odd[0]['number'] and x['name'].startswith(EDGE_AFTER)]
    return (all(any(x['name'].startswith(n) for x in after) for n in EDGE_AFTER)
            and all(x['conclusion'] == 'success' for x in after))


def known_edge_misses(files):
    """Whether the left-edge job's suites failed only by the known issue (CHANGELOG.md, v0.3.12): every one of the 288
    swipes ran once (EDGE_CASES, none skipped), 1 to EDGE_MAX_MISS of them didn't go back, and each of those has its own
    miss line saying it didn't go back late either and its page was still the one shown (late=0 stayed=1), on the same
    tab. files: the job's results.txt, ios-edge-back.log and edge-back.txt, or None when its artifact can't be read.
    Returns what to say, or None when the failure is anything else."""
    if not files or not all(files.get(k) for k in ('results.txt', 'ios-edge-back.log', 'edge-back.txt')):
        return None
    suites = [l.split()[:2] for l in files['results.txt'].splitlines() if l.strip()]
    if sorted(map(tuple, suites)) != [('ios-edge-back', 'FAIL'), ('ios-edge-record', 'PASS')]:
        return None
    if [l for l in files['ios-edge-back.log'].splitlines() if l.startswith('FAIL')] != ['FAIL: fewer than 100% of the edge swipes went back']:
        return None
    lines = files['edge-back.txt'].splitlines()
    swipes = {}
    for l in (l for l in lines if l.startswith('page=')):
        m = re.fullmatch(r'page=(\S+) y=(\S+) x=(\d+) v=(\S+) i=(\d+) back=([01]) tab=(same|changed)', l)
        key = m and '-'.join(m.group(1, 2, 3, 4, 5))
        if not m or key in swipes:
            return None
        swipes[key] = (m.group(6), m.group(7))
    if set(swipes) != EDGE_CASES:
        return None
    missed = {k for k, (back, _) in swipes.items() if back == '0'}
    told = [re.fullmatch(r'miss (\S+) late=0 stayed=1 shot=\S+', l) for l in lines if l.startswith('miss ')]
    if (not 1 <= len(missed) <= EDGE_MAX_MISS or not all(told) or sorted(m.group(1) for m in told) != sorted(missed)
            or any(swipes[k][1] != 'same' for k in missed)):
        return None
    return f'known issue, {len(missed)} of {len(swipes)} swipes back from the left edge did not start'


def problems(sha, api, want, notes=None):
    found = []
    notes = notes if notes is not None else []
    runs = [r for r in api(f'commits/{sha}/runs') if r.get('event') in EVENTS]
    newest = lambda rs: max(rs, key=lambda r: (r['created_at'], r['id'])) if rs else None
    # The newest whole run counts, finished or not: an older green one, or a green part of one, says nothing of it.
    def settled(what, r):
        if not r:
            found.append(f'{what}: no run on {sha}')
        elif r['status'] != 'completed':
            found.append(f'{what}: the newest run is still {r["status"]} ({r["html_url"]})')
        elif r['conclusion'] != 'success':
            found.append(f'{what}: the newest run ended {r["conclusion"]} ({r["html_url"]})')
        else:
            return True
        return False
    for name in ('Tests', 'Computers', 'History'):
        settled(name, newest([r for r in runs if r['name'] == name and r['event'] == 'push']))
    # A run's name is its run-name: devices.yml names its runs by their title, so a Devices run is told by its file.
    devices = [r for r in runs if r.get('path') == DEVICES_PATH]
    release = newest([r for r in devices if is_release(r)])
    if not release:
        found.append(f'Devices: no release run on {sha} (dispatch devices.yml with groups=release, and no other input, on its branch)')
    # A release run that failed only by the known issue ended in failure too: its jobs say which.
    elif release['status'] == 'completed' and release['conclusion'] in ('success', 'failure'):
        jobs = api(f'runs/{release["id"]}/jobs')
        by = {j['name']: j for j in jobs}
        known = None
        bad = [j for j in jobs if j['conclusion'] not in ('success', 'skipped')]
        if release['conclusion'] == 'failure' and not bad:
            found.append(f'Devices release: the newest run ended failure ({release["html_url"]})')
            want = []
        if EDGE_JOB in want and known_edge_job(by.get(EDGE_JOB, {})):
            note = known_edge_misses(api(f'artifact/{release["id"]}/{EDGE_ARTIFACT}'))
            if note:
                known = EDGE_JOB
                notes.append(f'{EDGE_JOB}: {note} ({release["html_url"]})')
        for n in want:
            if n != known and by.get(n, {}).get('conclusion') != 'success':
                found.append(f'Devices: the release run did not pass {n} ({release["html_url"]})')
        # Any other job that ran and didn't pass; a job skipped by its condition is fine.
        for j in bad:
            if j['name'] not in want:
                found.append(f'Devices: the release run\'s job {j["name"]} ended {j["conclusion"]} ({release["html_url"]})')
    else:
        settled('Devices release', release)
    for r in devices:
        if r is not release and r['status'] == 'completed' and r['conclusion'] == 'failure':
            found.append(f'Devices: a run on this commit failed, run it again to green or find why ({r["html_url"]})')
    return found


def live_api(repo):
    def api(path):
        if path.startswith('artifact/'):
            _, run, name = path.split('/')
            with tempfile.TemporaryDirectory() as d:
                got = subprocess.run(['gh', 'run', 'download', run, '--repo', repo, '-n', name, '-D', d], capture_output=True, text=True)
                if got.returncode != 0:
                    return None
                files = {}
                for f in ('results.txt', 'ios-edge-back.log', 'edge-back.txt'):
                    p = os.path.join(d, f)
                    files[f] = open(p, errors='replace').read() if os.path.isfile(p) else ''
                return files
        if path.startswith('commits/'):
            sha = path.split('/')[1]
            got, page = [], 1
            while True:
                b = gh(f'repos/{repo}/actions/runs?head_sha={sha}&per_page=100&page={page}')
                got += b['workflow_runs']
                if len(got) >= b['total_count'] or not b['workflow_runs']:
                    return got
                page += 1
        if path.startswith('runs/'):
            got, page = [], 1
            while True:
                b = gh(f'repos/{repo}/actions/{path}?per_page=100&page={page}')
                got += b['jobs']
                if len(got) >= b['total_count'] or not b['jobs']:
                    return got
                page += 1
        raise ValueError(path)
    return api


def self_test():
    sha = 'a' * 40
    # As GitHub gives a run: its name is the workflow's run-name, which for devices.yml is the run's title.
    def run(i, workflow, title, conclusion='success', status='completed', event='push', at='2026-11-25T10:00:00Z'):
        return {'id': i, 'name': title if workflow == 'Devices' else workflow, 'path': f'.github/workflows/{workflow.lower()}.yml',
                'display_title': title, 'conclusion': conclusion, 'status': status, 'event': event,
                'created_at': at, 'html_url': f'https://example.com/{i}'}
    want = ['iOS a', 'iOS edge', EDGE_JOB, 'Android x', 'Android update', 'A picture elsewhere', 'Install steps']
    later = '2026-11-25T11:00:00Z'
    good = [run(1, 'Tests', 'x'), run(2, 'Computers', 'x'), run(3, 'History', 'x'), run(4, 'Devices', 'x'),
            run(5, 'Devices', 'Devices groups release', event='workflow_dispatch')]
    every = [{'name': n, 'conclusion': 'success'} for n in want] + [{'name': 'Which suite groups run', 'conclusion': 'success'}]
    jobs = {5: every}
    # The left-edge job's artifact: the 288 swipes as EdgeBack.swift makes them, by number in this grid. miss: those
    # that didn't go back; late, moved, tab: of those, back late, page no longer shown, tab changed; skip, drop, twice:
    # a swipe whose page didn't open, left out, written twice; label: a miss line naming the next swipe instead.
    grid = [(p, y, x, v, i) for p in ('space', 'space-asking', 'settings', 'space-settings') for y in ('header', 'middle', 'low')
            for x in (2, 12) for v in ('slow', 'fast') for i in range(1, 7)]
    def edge(miss=(), late=(), moved=(), tab=(), skip=(), drop=(), twice=(), label=(), extra=(),
             results='ios-edge-back FAIL 2885s\nios-edge-record PASS 281s\n', log='FAIL: fewer than 100% of the edge swipes went back\n'):
        name = lambda n: '-'.join(map(str, grid[n]))
        lines = ['os 26.5 screen 402x874']
        for n, (pg, y, x, v, i) in enumerate(grid):
            if n in drop:
                continue
            if n in skip:
                lines.append(f'page={pg} y={y} x={x} v={v} i={i} back=skip (page did not open)')
                continue
            lines.append(f'page={pg} y={y} x={x} v={v} i={i} back={0 if n in miss else 1} tab={"changed" if n in tab else "same"}')
            if n in twice:
                lines.append(lines[-1])
            if n in miss or n in extra:
                lines.append(f'miss {name(n + 1 if n in label else n)} late={int(n in late)} stayed={int(n not in moved)} shot=x.png')
        return {'results.txt': results, 'ios-edge-back.log': log, 'edge-back.txt': '\n'.join(lines) + '\n'}
    def fake(rs, js=jobs, art=None):
        return lambda p: rs if p.startswith('commits/') else art if p.startswith('artifact/') else js.get(int(p.split('/')[1]), [])
    # The left-edge job as GitHub lists it when only its Suites step failed; steps: other conclusions by step name,
    # None to leave the step out.
    def edge_job(conclusion='failure', **steps):
        names = [(1, 'Set up job', 'success'), (8, 'Run app/e2e/ci/tools.sh omp', 'skipped'), (10, 'Suites', 'failure'),
                 (11, 'Check what is uploaded', 'success'), (12, 'Shut the simulator', 'success'),
                 (13, 'Run actions/upload-artifact@cf43', 'success'), (24, 'Post Run actions/setup-go@b7ad', 'skipped'),
                 (27, 'Complete job', 'success')]
        key = lambda n: re.sub(r'[^A-Za-z0-9]', '_', n.split('@')[0])
        got = [{'number': i, 'name': n, 'conclusion': steps.get(key(n), c)} for i, n, c in names]
        return {'name': EDGE_JOB, 'conclusion': conclusion, 'steps': [x for x in got if x['conclusion'] is not None]}
    def run_jobs(edge_as=None, **others):
        """The release run's jobs: the left edge job as edge_as (passed when None), the others by name as given."""
        out = [edge_as if j['name'] == EDGE_JOB and edge_as else dict(j, conclusion=others.get(j['name'], j['conclusion'])) for j in every]
        return {5: out + [{'name': n, 'conclusion': c} for n, c in others.items() if n not in want and n != 'Which suite groups run']}
    edge_failed = run_jobs(edge_job())
    skip = lambda *names: {5: [dict(j, conclusion='skipped') if j['name'] in names else j for j in every]
                               + [{'name': 'iOS a (copy 1)', 'conclusion': 'success'}, {'name': 'Android y', 'conclusion': 'success'}]}
    cases = [
        ('all green', good, jobs, []),
        # Three groups skipped while others pass, more jobs than groups in all.
        ('three groups skipped', good, skip('iOS edge', 'A picture elsewhere', 'Install steps'), ['did not pass iOS edge', 'did not pass A picture elsewhere', 'did not pass Install steps']),
        ('a group failed', good, {5: [dict(j, conclusion='failure') if j['name'] == 'Android x' else j for j in every]}, ['did not pass Android x']),
        # The newest whole run is what counts.
        ('Tests failed, then a part of it passed', [run(1, 'Tests', 'x', 'failure')] + good[1:] + [run(9, 'Tests', 'Tests', event='workflow_dispatch', at=later)], jobs, ['Tests: the newest run ended failure']),
        ('a green Tests, and a newer one still running', good + [run(9, 'Tests', 'x', None, 'in_progress', at=later)], jobs, ['Tests: the newest run is still in_progress']),
        ('a green release run, and a newer one still running', good + [run(9, 'Devices', 'Devices groups release', None, 'queued', 'workflow_dispatch', later)], jobs, ['Devices release: the newest run is still queued']),
        ('a failed release run after a green one', good + [run(9, 'Devices', 'Devices nightly', 'failure', event='schedule', at=later)], jobs, ['Devices release: the newest run ended failure']),
        ('Tests passed again later', [run(9, 'Tests', 'x', 'failure', at='2026-11-25T09:00:00Z')] + good, jobs, []),
        ('only a pull request ran Computers', [good[0], run(2, 'Computers', 'x', event='pull_request')] + good[2:], jobs, ['Computers: no run']),
        ('only a dispatch ran History', good[:2] + [run(3, 'History', 'History', event='workflow_dispatch')] + good[3:], jobs, ['History: no run']),
        # The release groups as they are, by event and the whole title.
        ('nightly counts', good[:4] + [run(5, 'Devices', 'Devices nightly', event='schedule')], jobs, []),
        ('release with BEFORE pictures', good[:4] + [run(5, 'Devices', 'Devices groups release pin_order_before', event='workflow_dispatch')], jobs, ['no release run']),
        ('release with away cycles', good[:4] + [run(5, 'Devices', 'Devices groups release chat_away_cycles=3', event='workflow_dispatch')], jobs, ['no release run']),
        ('release and another group', good[:4] + [run(5, 'Devices', 'Devices groups release, ios-menus', event='workflow_dispatch')], jobs, ['no release run']),
        ('copies are no release run', good[:4] + [run(5, 'Devices', 'Devices ios_b_repeat=3', event='workflow_dispatch')], jobs, ['no release run']),
        ("a nightly title from a dispatch", good[:4] + [run(5, 'Devices', 'Devices nightly', event='workflow_dispatch')], jobs, ['no release run']),
        ("a release title from a push", good[:4] + [run(5, 'Devices', 'Devices groups release')], jobs, ['no release run']),
        ('no release run', good[:4], jobs, ['no release run']),
        ('another Devices run failed', good + [run(6, 'Devices', 'x', 'failure')], jobs, ['a run on this commit failed']),
        ('a cancelled one is not a failure', good + [run(6, 'Devices', 'x', 'cancelled')], jobs, []),
    ]
    # The known left-edge issue: (what, conclusion of the edge job, its artifact, problems, whether it is a note).
    # The known left-edge issue: (what, the run's jobs, the edge job's artifact, problems, whether it is a note).
    E = 'did not pass ' + EDGE_JOB
    edge_cases = [
        ('one swipe back did not start', edge_failed, edge(miss=(3,)), [], True),
        ('three, the most', edge_failed, edge(miss=(3, 140, 287)), [], True),
        ('four', edge_failed, edge(miss=(3, 40, 140, 287)), [E], False),
        ('a miss that went back late', edge_failed, edge(miss=(3,), late=(3,)), [E], False),
        ("a miss whose page wasn't shown any more", edge_failed, edge(miss=(3,), moved=(3,)), [E], False),
        ('a miss that changed the tab', edge_failed, edge(miss=(3,), tab=(3,)), [E], False),
        ("a swipe whose page didn't open", edge_failed, edge(miss=(3,), skip=(50,)), [E], False),
        ('a swipe left out', edge_failed, edge(miss=(3,), drop=(50,)), [E], False),
        ('a swipe written twice', edge_failed, edge(miss=(3,), twice=(50,)), [E], False),
        ('a miss line naming another swipe', edge_failed, edge(miss=(3,), label=(3,)), [E], False),
        ('a miss line for a swipe that went back', edge_failed, edge(miss=(3,), extra=(9,)), [E], False),
        ('the record suite failed too', edge_failed, edge(miss=(3,), results='ios-edge-back FAIL 1s\nios-edge-record FAIL 1s\n'), [E], False),
        ('the test itself broke', edge_failed, edge(miss=(3,), log='FAIL: the EdgeBack test (xcodebuild test exit 65)\n'), [E], False),
        ('no artifact', edge_failed, None, [E], False),
        ('Suites and Shut the simulator both failed', run_jobs(edge_job(Shut_the_simulator='failure')), edge(miss=(3,)), [E], False),
        ('Suites and a post step failed', run_jobs(edge_job(Post_Run_actions_setup_go='failure')), edge(miss=(3,)), [E], False),
        ('only Shut the simulator failed', run_jobs(edge_job(Suites='success', Shut_the_simulator='failure')), edge(miss=(3,)), [E], False),
        ('the upload did not run', run_jobs(edge_job(Run_actions_upload_artifact='skipped')), edge(miss=(3,)), [E], False),
        ('no upload step', run_jobs(edge_job(Run_actions_upload_artifact=None)), edge(miss=(3,)), [E], False),
        ('a job outside the release groups timed out', run_jobs(edge_job(), **{'iOS a (copy 1)': 'timed_out'}), edge(miss=(3,)), ['iOS a (copy 1) ended timed_out'], True),
        ('the plan job failed', run_jobs(edge_job(), **{'Which suite groups run': 'failure'}), edge(miss=(3,)), ['Which suite groups run ended failure'], True),
        ('a cancelled edge job', run_jobs(edge_job('cancelled', Suites='cancelled')), edge(miss=(3,)), ['the newest run ended cancelled'], False),
        ('the edge job skipped while another failed', run_jobs(edge_job('skipped', Suites='skipped'), **{'Android x': 'failure'}), edge(miss=(3,)), [E, 'did not pass Android x'], False),
        ('another iOS job failed beside it', run_jobs(edge_job(), **{'iOS a': 'failure'}), edge(miss=(3,)), ['did not pass iOS a'], True),
    ]
    fail = 0
    for what, rs, js, needs in cases:
        got = problems(sha, fake(rs, js), want)
        ok = len(got) == len(needs) and all(any(n in g for g in got) for n in needs)
        if not ok:
            print(f'FAIL: release-gate self-test, {what}: {got}')
            fail = 1
    for what, js, art, needs, noted in edge_cases:
        notes = []
        # GitHub ends a run with a failed job in failure, and one with a cancelled job in cancelled.
        ends = {j['conclusion'] for j in js[5]}
        end = 'failure' if ends & {'failure', 'timed_out'} else 'cancelled' if 'cancelled' in ends else 'success'
        red = good[:4] + [run(5, 'Devices', 'Devices groups release', end, event='workflow_dispatch')]
        got = problems(sha, fake(red, js, art), want, notes)
        ok = (len(got) == len(needs) and all(any(n in g for g in got) for n in needs)
              and bool(notes) == noted and all('known issue' in n for n in notes))
        if not ok:
            print(f'FAIL: release-gate self-test, {what}: {got} {notes}')
            fail = 1
    names = expected_jobs()
    counts = [sum(n.startswith('iOS ') for n in names), sum(n.startswith('Android ') and n not in FLAG_JOBS['update'] for n in names),
              sum(any(n in jobs for jobs in FLAG_JOBS.values()) for n in names)]
    if counts != [15, 14, 4] or len(set(names)) != len(names):
        print(f'FAIL: release-gate self-test: the release jobs are {counts} (iOS, Android, update/remote/user_path), not [15, 14, 4]: {names}')
        fail = 1
    yml = open(DEVICES).read()
    for broken in (yml.replace("\n    name: A picture on another machine (docker)\n", "\n    name: A picture elsewhere\n"),
                   yml.replace('    name: iOS ${{ fromJSON(', '    name: iOS ${{ format(')):
        try:
            expected_jobs(broken)
            print('FAIL: release-gate self-test: a devices.yml without a job it names still gives the release jobs')
            fail = 1
        except SystemExit:
            pass
    print('FAIL: release-gate self-test' if fail else 'PASS: release-gate self-test')
    return fail


def main(argv):
    if argv[1:] == ['--self-test']:
        return self_test()
    args = argv[1:]
    repo = REPO
    if '--repo' in args:
        i = args.index('--repo')
        repo = args[i + 1]
        del args[i:i + 2]
    if len(args) != 1 or not re.fullmatch(r'[0-9a-f]{40}', args[0]):
        print(__doc__.strip(), file=sys.stderr)
        return 2
    notes = []
    found = problems(args[0], live_api(repo), expected_jobs(), notes)
    for n in notes:
        print(n)
    for p in found:
        print(p)
    print(f'{len(found)} problems for {args[0]} on {repo}')
    return 1 if found else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
