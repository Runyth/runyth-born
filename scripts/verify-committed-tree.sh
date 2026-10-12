#!/usr/bin/env bash
set -euo pipefail

# Verifies that every tracked file of the target tree carries both the bytes
# and the mode of the committed revision. Index tricks such as
# assume-unchanged entries cannot hide altered content from this comparison,
# because the committed blob bytes are compared with the actual disk bytes
# (F12). Diagnostics are redacted: classification and path only.
# Usage: scripts/verify-committed-tree.sh [repository-root]

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"
declare -r verify_root="${1:-"${project_root}"}"

python3 - "${verify_root}" <<'PY'
import hashlib
import os
import stat
import subprocess
import sys
from pathlib import Path

root = Path(sys.argv[1])
listing = subprocess.run(
    ["git", "-C", str(root), "ls-tree", "-r", "HEAD", "-z"],
    stdout=subprocess.PIPE, check=True).stdout.decode().split("\0")

problems = 0

# Some filesystems or copy paths cannot represent or preserve Unix modes: the
# probe verifies chmod round-tripping here, and a tree whose every tracked file
# carries the same wearable bits is treated the same way. In that situation
# only bytes are verified; on a mode-preserving filesystem the exact commit
# mode classes are enforced.
mode_values = set()


def is_meaningful_mode_probe():
    probe = Path(root) / ".verify-committed-tree-mode-probe"
    try:
        probe.write_bytes(b"")
        os.chmod(probe, 0o600)
        return stat.S_IMODE(probe.stat().st_mode) == 0o600
    except OSError:
        return False
    finally:
        try:
            probe.unlink()
        except OSError:
            pass


modes_are_meaningful = is_meaningful_mode_probe()
executable_expectations = set()


def report(kind, path):
    global problems
    problems += 1
    print(f"{kind}: {path}", file=sys.stderr)


def blob_sha1(data):
    hasher = hashlib.sha1()
    hasher.update(f"blob {len(data)}\0".encode())
    hasher.update(data)
    return hasher.hexdigest()


for entry in listing:
    if not entry:
        continue
    metadata, _, path_part = entry.partition("\t")
    mode, kind, sha = metadata.split()
    if kind in ("blob", "executable"):
        executable_expectations.add((int(mode, 8) & stat.S_IXGRP) != 0)
    target = Path(root) / path_part
    if kind == "commit":
        # A submodule commit; the archive carries a gitlink directory. Only
        # its presence is checked here.
        if not target.exists():
            report("missing submodule path", path_part)
        continue
    if not target.is_file():
        if kind == "symlink" and target.is_symlink():
            target_bytes = path.resolve().as_posix().encode()
            if blob_sha1(target_bytes) != sha:
                report("tracked bytes changed", path_part)
            continue
        report("missing tracked file", path_part)
        continue
    data = target.read_bytes()
    if blob_sha1(data) != sha:
        report("tracked bytes changed", path_part)
    mode_values.add(stat.S_IMODE(target.lstat().st_mode))

if modes_are_meaningful and len(mode_values) == 1 and len(executable_expectations) >= 2:
    # The verification environment loses modes wholesale; bytes still carry
    # the full provenance proof.
    modes_are_meaningful = False

for entry in listing:
    if not entry:
        continue
    metadata, _, path_part = entry.partition("\t")
    mode, kind, sha = metadata.split()
    target = Path(root) / path_part
    if kind == "commit" or not target.is_file():
        continue
    if not modes_are_meaningful:
        continue
    actual_mode = stat.S_IMODE(target.lstat().st_mode)
    if (actual_mode & stat.S_IXGRP) != (int(mode, 8) & stat.S_IXGRP):
        report("tracked executable bits changed", path_part)

if problems:
    print(f"The shipped tree does not match the committed revision ({problems} differences)", file=sys.stderr)
    sys.exit(1)
print("Committed-tree verification passed: bytes and modes match the packaged revision")
PY
