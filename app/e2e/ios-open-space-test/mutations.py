"""Prove that removing each strict failure or the first-miss record turns the tests red."""
from pathlib import Path
import subprocess
import tempfile

e2e = Path(__file__).resolve().parents[1]
script = e2e / "ios-open-space-test.sh"
source = (e2e / "ios-uitest/tests/OpenSpace.swift").read_text()
subprocess.run(["sh", str(script)], check=True)
mutations = [
    ("menu", 'XCTFail("\\(stage): \\(why)")',
     'if why != "long-press menu" { XCTFail("\\(stage): \\(why)") }', "FAIL: menu:"),
    ("exit", 'XCTFail("\\(stage): \\(why)")',
     'if why != "process exited" { XCTFail("\\(stage): \\(why)") }', "FAIL: exited:"),
    ("final-open", 'XCTAssertTrue(shown, "the space or terminal did not open")',
     '// final check removed', "FAIL: never-opens:"),
    ("self-test", 'XCTFail("intentional first-space miss (self-test)")',
     '// self-test check removed', "FAIL: intentional-miss:"),
    ("first-record", 'note("first space tap: still on home; KNOWN ISSUE in 0.2.0")',
     '// first miss record removed', "FAIL: known-home:"),
]
with tempfile.TemporaryDirectory() as work:
    for name, before, after, expected in mutations:
        assert source.count(before) == 1, (name, "mutation no longer matches once")
        path = Path(work) / "OpenSpace.swift"
        path.write_text(source.replace(before, after))
        result = subprocess.run(["sh", str(script), str(path)], capture_output=True, text=True)
        assert result.returncode == 1 and expected in result.stdout, (name, result.stdout, result.stderr)
        print(f"PASS: mutation {name} caught by {expected}")
