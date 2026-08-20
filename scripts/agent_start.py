#!/usr/bin/env python3
"""Read-only session bootstrap. Prints git + protocol + working-state files."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

FILES = [
    ROOT / "AGENTS.md",
    ROOT / ".agent" / "STATE.md",
    ROOT / ".agent" / "DECISIONS.md",
    ROOT / ".agent" / "TODO.md",
    ROOT / ".agent" / "HANDOFF.md",
]


def git(*args: str) -> str:
    r = subprocess.run(
        ["git", *args],
        cwd=ROOT,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    out = (r.stdout or "").strip()
    err = (r.stderr or "").strip()
    if r.returncode != 0:
        return f"(git {' '.join(args)} failed: {err or r.returncode})"
    return out


def banner(title: str) -> None:
    print()
    print("=" * 72)
    print(title)
    print("=" * 72)


def main() -> int:
    banner("git status")
    print(git("status"))
    banner("git log -8")
    print(git("log", "-8", "--oneline"))
    for path in FILES:
        banner(str(path.relative_to(ROOT)))
        if not path.is_file():
            print(f"(missing: {path})")
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        print(text, end="" if text.endswith("\n") else "\n")
    banner("canonical handoff")
    print("Read docs/PROJECT_HANDOFF.md before material changes.")
    print("Chat is temporary. Git = code. .agent/ = working state.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
