#!/usr/bin/env bash
set -euo pipefail

# Verifies that every tracked file of the target tree carries both the bytes
# and the mode of the committed revision. Index tricks such as
# assume-unchanged entries cannot hide altered content from this comparison,
# because the committed blob bytes are compared with the actual disk bytes
# (F12). Diagnostics are redacted: classification and path only.
# Mode verification uses the owner execute class. On a filesystem that cannot
# represent Unix modes (detected by a chmod round-trip probe on the target and
# the class-verified origin), only bytes are verified and the success message
# reports the reduced scope honestly (F12).
# Usage: scripts/verify-committed-tree.sh <repository-root> [origin-root]

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"
declare -r verify_root="${1:-"${project_root}"}"
declare -r origin_root="${2:-}"

python3 - "${verify_root}" "${origin_root}" <<'PY'
import hashlib
import os
import stat
import subprocess
import sys
from pathlib import Path

root = Path(sys.argv[1])
origin = Path(sys.argv[2]) if len(sys.argv) > 2 and sys.argv[2] else None
listing = subprocess.run(
    ["git", "-C", str(root), "ls-tree", "-r", "HEAD", "-z"],
    stdout=subprocess.PIPE, check=True).stdout.decode().split("\0")

problems = 0


def chmod_probe(base):
    probe = base / ".verify-committed-tree-mode-probe"
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


modes_are_meaningful = chmod_probe(root) and (origin is None or chmod_probe(origin))


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
    target = Path(root) / path_part
    if kind == "commit":
        # A submodule commit; the archive carries a gitlink directory. Only
        # its presence is checked here.
        if not target.exists():
            report("missing submodule path", path_part)
        continue
    if not target.is_file():
        if kind == "symlink" and target.is_symlink():
            resolved = target.resolve().as_posix().encode()
            if blob_sha1(resolved) != sha:
                report("tracked bytes changed", path_part)
            continue
        report("missing tracked file", path_part)
        continue
    data = target.read_bytes()
    if blob_sha1(data) != sha:
        report("tracked bytes changed", path_part)
    if not modes_are_meaningful:
        continue
    actual_mode = stat.S_IMODE(target.lstat().st_mode)
    committed_executable = (int(mode, 8) & stat.S_IXUSR) != 0
    if ((actual_mode & stat.S_IXUSR) != 0) != committed_executable:
        report("tracked executable bits changed", path_part)

if problems:
    print(f"The shipped tree does not match the committed revision ({problems} differences)", file=sys.stderr)
    sys.exit(1)
if modes_are_meaningful:
    print("Committed-tree verification passed: bytes and modes match the packaged revision")
else:
    print("Committed-tree verification passed: bytes match the packaged revision; mode verification unavailable for this filesystem")
PY
