"""Check ChatFour's completed 60-second cycles; a partial log is a failure."""
from pathlib import Path
import sys


def check(lines, count):
    if not 1 <= count <= 30:
        raise ValueError("expected 1..30 background cycles")
    events = [line.strip() for line in lines if line.startswith("away-cycle ")]
    totals = tuple(sum(line.startswith(f"away-cycle {stage} ") for line in events)
                   for stage in ("start", "resume", "pass"))
    expected = [f"away-cycle {stage} {n}/{count} seconds=60"
                for n in range(1, count + 1) for stage in ("start", "resume", "pass")]
    if events != expected:
        raise ValueError(f"background cycles incomplete or out of order: started/resumed/passed={totals}, expected={count}")
    return totals


if __name__ == "__main__":
    try:
        count = int(sys.argv[1])
        totals = check(Path(sys.argv[2]).read_text().splitlines(), count)
    except (ValueError, OSError) as error:
        sys.exit(f"FAIL: {error}")
    print(f"PASS: {count} background cycles, 60 seconds each; started/resumed/passed={totals}")
