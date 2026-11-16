"""Keep one iOS test build's raw source private and publish redacted, bound evidence."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def digest(data):
    return hashlib.sha256(data).hexdigest()


def capture(source, target):
    target.mkdir(mode=0o700, parents=True)
    for name in ['bundle.js', 'bundle.js.map', 'bundle.nshc']:
        path = source / name
        if name.endswith('.map') and not path.exists(): continue
        data = path.read_bytes()
        dest = target / name
        dest.write_bytes(data)
        dest.chmod(0o600)


def redact(data, link):
    safe = data.replace(link, b'x' * len(link))
    assert len(safe) == len(data)
    assert [i for i, v in enumerate(safe) if v == 10] == [i for i, v in enumerate(data) if v == 10]
    assert [len(s.encode('utf-16-le')) for s in safe.decode().splitlines()] == [len(s.encode('utf-16-le')) for s in data.decode().splitlines()]
    # bytes.replace changes only exact matches, each to the same number of ASCII bytes.
    return safe


def private_text(text):
    # FakeCore's exact public demo home is approved in source-export.toml.
    checked = re.sub(r'/Users/sam(?![A-Za-z0-9_./-])', '', text)
    return bool(re.search(r'/(?:Users|home|Volumes|private|var/folders)/|[A-Za-z]:\\Users\\|#pair=[A-Za-z0-9_-]+', checked))


def clean_map(data, link, example, app):
    mapping = json.loads(data)
    root = mapping.get('sourceRoot', '')
    sources = []
    for name in mapping['sources']:
        # Virtual modules have no host filesystem path.
        if name.startswith(('nsh:', '\x00')):
            sources.append(name)
            continue
        path = Path(root) / name
        if not path.is_absolute(): path = app / path
        try: sources.append(path.resolve().relative_to(app.parent.resolve()).as_posix())
        except ValueError: raise ValueError('source map has an unrelated absolute source path') from None
    mapping['sources'] = sources
    mapping['sourceRoot'] = ''
    if 'sourcesContent' in mapping:
        mapping['sourcesContent'] = [redact(redact(s.encode(), link), example).decode() if s is not None else None for s in mapping['sourcesContent']]
    public = {key: value for key, value in mapping.items() if key != 'sourcesContent'}
    result = json.dumps(public, ensure_ascii=False).encode()
    if link in result or private_text(result.decode()):
        raise ValueError('source map contains a pairing link or private path')
    return result, mapping


def segments(mapping):
    alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
    source = original_line = original_col = 0
    for generated_line, line in enumerate(mapping['mappings'].split(';'), 1):
        column = 0
        for segment in line.split(','):
            if not segment: continue
            values = []; value = shift = 0
            for char in segment:
                digit = alphabet.index(char)
                value |= (digit & 31) << shift
                if digit & 32: shift += 5; continue
                values.append(-(value >> 1) if value & 1 else value >> 1)
                value = shift = 0
            column += values[0]
            if len(values) < 4: continue
            source += values[1]; original_line += values[2]; original_col += values[3]
            yield generated_line, column + 1, source, original_line + 1, original_col + 1


def known_sites(mapping):
    wanted = [('app/src/ui/root.tsx', 'const offConn = store.conn.sub'),
              ('app/src/ui/kit.tsx', 'const off = s.sub')]
    result = []
    mapped = list(segments(mapping))
    for file, code in wanted:
        index = mapping['sources'].index(file)
        lines = mapping['sourcesContent'][index].splitlines()
        line = next(i + 1 for i, text in enumerate(lines) if code in text)
        candidates = [s for s in mapped if s[2] == index and s[3] == line]
        if not candidates: raise ValueError('source map does not map a known subscription')
        generated, col, _, original, original_col = candidates[0]
        result.append({'site': f'bundle.js:{generated}:{col}', 'source': file,
                       'line': original, 'column': original_col})
    if result[0]['site'] == result[1]['site']: raise ValueError('known subscription coordinates are identical')
    return result


def publish(raw, installed, out, secret, app):
    link = secret.read_bytes().strip()
    if not link or not link.isascii() or b'\n' in link or b'\r' in link:
        raise ValueError('pair link must be single-line ASCII')
    bytecode = (raw / 'bundle.nshc').read_bytes()
    installed_bytes = installed.read_bytes()
    if bytecode != installed_bytes: raise ValueError('installed bytecode differs from captured build')
    original = (raw / 'bundle.js').read_bytes()
    count = original.count(link)
    if not count: raise ValueError('known pair link is absent from bundle; check compiled representation')
    example_line = next(line for line in (app / 'src/ui/pair.tsx').read_text().splitlines()
                        if "t('pasteLan', { example:" in line)
    example = example_line.split("example: '", 1)[1].split("'", 1)[0].encode('ascii')
    # The exact public example reviewed in PairScreen; no other invite is exempted.
    if digest(example) != 'fdadba2c2a184f1543804f76ebedd653c63d403c0c2a0c5df16322030b0ec3dc':
        raise ValueError('public pairing example changed')
    timer_assignment = b'_token=setTimeout'
    safe = redact(redact(redact(original, link), example), timer_assignment)
    if link in safe or private_text(safe.decode()): raise ValueError('bundle contains a pairing link or private path')
    files = {'bundle.js': safe}
    fingerprints = {'bundle.js': {'original_sha256': digest(original), 'published_sha256': digest(safe)}}
    map_file = raw / 'bundle.js.map'
    if map_file.exists():
        map_raw = map_file.read_bytes()
        map_safe, mapping = clean_map(map_raw, link, example, app)
        files['bundle.js.map'] = map_safe
        fingerprints['bundle.js.map'] = {'original_sha256': digest(map_raw), 'published_sha256': digest(map_safe)}
        files['known-sites.json'] = (json.dumps(known_sites(mapping), indent=2) + '\n').encode()
    sha = subprocess.check_output(['git', '-C', str(app), 'rev-parse', 'HEAD'], text=True).strip()
    manifest = {'source_sha': sha, 'entry': 'ios-e2e.tsx', 'files': fingerprints,
                'bytecode_sha256': digest(bytecode), 'installed_bytecode_sha256': digest(installed_bytes),
                'link_replacements': count, 'approved_example_replacements': original.count(example),
                'timer_assignment_replacements': original.count(timer_assignment), 'map': 'present' if map_file.exists() else 'absent',
                'same_build': True, 'byte_length_newlines_utf16_columns_preserved': True}
    files['manifest.json'] = (json.dumps(manifest, indent=2) + '\n').encode()
    shape_line = next(line for line in (app / 'e2e/ci-shots-check.sh').read_text().splitlines()
                      if line.startswith("shapes='"))
    shapes = shape_line.split("'", 2)[1].encode()
    if any(re.search(shapes, data) for data in files.values()):
        raise ValueError('published evidence matches the CI pairing-secret shapes')
    # Nothing enters the upload directory until all checks above passed.
    out.mkdir(parents=True)
    for name, data in files.items(): (out / name).write_bytes(data)


def main():
    if sys.argv[1] == 'capture': capture(Path(sys.argv[2]), Path(sys.argv[3]))
    elif sys.argv[1] == 'publish': publish(*(Path(p) for p in sys.argv[2:]))
    else: raise ValueError('unknown evidence command')


if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, KeyError, StopIteration) as error:
        # Never echo the input link, source paths, or source contents.
        print('subscriber source evidence failed: ' + type(error).__name__, file=sys.stderr)
        sys.exit(1)
