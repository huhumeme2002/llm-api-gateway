#!/usr/bin/env python3
"""Archive HANDOFF.md, write a fresh template, print git status/diff for the agent to fill."""

from __future__ import annotations

import datetime as dt
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
AGENT = ROOT / ".agent"
HANDOFF = AGENT / "HANDOFF.md"
HISTORY = AGENT / "handoff_history"

TEMPLATE = """# HANDOFF

## Last agent

(who / which CLI)

## Changed

-

## Still broken

-

## Next

1.

## Do not

-
"""


def git(*args: str) -> str:
    r = subprocess.run(
        ["git", *args],
        cwd=ROOT,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    out = (r.stdout or "").rstrip()
    err = (r.stderr or "").rstrip()
    if r.returncode != 0:
        return f"(git {' '.join(args)} failed: {err or r.returncode})"
    return out


def banner(title: str) -> None:
    print()
    print("=" * 72)
    print(title)
    print("=" * 72)


def main() -> int:
    AGENT.mkdir(parents=True, exist_ok=True)
    HISTORY.mkdir(parents=True, exist_ok=True)

    if HANDOFF.is_file() and HANDOFF.read_text(encoding="utf-8", errors="replace").strip():
        stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        dest = HISTORY / f"HANDOFF-{stamp}.md"
        shutil.copy2(HANDOFF, dest)
        print(f"archived {HANDOFF.relative_to(ROOT)} -> {dest.relative_to(ROOT)}")
    else:
        print("no existing HANDOFF.md to archive")

    HANDOFF.write_text(TEMPLATE, encoding="utf-8", newline="\n")
    print(f"wrote fresh template {HANDOFF.relative_to(ROOT)}")
    print()
    print("Fill .agent/STATE.md, .agent/TODO.md, and .agent/HANDOFF.md.")
    print("If runtime/schema/safety/policy changed, update docs/PROJECT_HANDOFF.md in the same commit.")
    print("Commit .agent/ with the work it describes. No conversation dumps, no secrets.")

    banner("git status")
    print(git("status"))
    banner("git diff")
    print(git("diff"))
    banner("git diff --stat")
    print(git("diff", "--stat"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
