"""Export only the test app's connection trace, never its pairing or preferences."""
import json
from pathlib import Path
import sys

store, output = map(Path, sys.argv[1:])
if store.is_file():
    saved = json.loads(store.read_text())
    key = 'e2e-connection-diag'
    value = saved.get(key)
    events = json.loads(value) if value is not None else []
    launches = sorted((int(k[len(key) + 1:]), v) for k, v in saved.items()
                      if k.startswith(key + '-') and k[len(key) + 1:].isdigit())
    for launch, value in launches:
        events.extend({**event, 'launch': launch} for event in json.loads(value))
    if value is not None or launches:
        output.write_text(json.dumps(events, indent=2) + '\n')
