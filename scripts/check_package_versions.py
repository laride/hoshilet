#!/usr/bin/env python3

"""Check npm/Python package versions and validate vSemVer tags in CI."""

import json
import os
import re
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SEMVER_TAG = re.compile(
    r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?"
    r"(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$"
)


def main() -> int:
    npm_metadata = json.loads(
        (ROOT / "cli/npm/package.json").read_text(encoding="utf-8")
    )
    python_metadata = tomllib.loads(
        (ROOT / "cli/python/pyproject.toml").read_text(encoding="utf-8")
    )
    npm_version = npm_metadata["version"]
    python_version = python_metadata["project"]["version"]

    if npm_version != python_version:
        print(
            f"npm version ({npm_version}) and Python version ({python_version}) differ",
            file=sys.stderr,
        )
        return 1

    if (
        os.getenv("GITHUB_EVENT_NAME") == "push"
        and os.getenv("GITHUB_REF_TYPE") == "tag"
    ):
        tag = os.getenv("GITHUB_REF_NAME", "")
        if not SEMVER_TAG.fullmatch(tag):
            print(f"Tag must be vSemVer: {tag}", file=sys.stderr)
            return 1

    output_path = os.getenv("GITHUB_OUTPUT")
    if output_path:
        with Path(output_path).open("a", encoding="utf-8") as output:
            output.write(f"npm={npm_version}\npython={python_version}\n")
    else:
        print(f"npm version: {npm_version}")
        print(f"Python version: {python_version}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
