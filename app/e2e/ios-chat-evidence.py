"""Export only the test app's connection trace, never its pairing or preferences."""
import json
from pathlib import Path
import sys

store, output = map(Path, sys.argv[1:])
if store.is_file():
    value = json.loads(store.read_text()).get('e2e-connection-diag')
    if value is not None:
        output.write_text(json.dumps(json.loads(value), indent=2) + '\n')
