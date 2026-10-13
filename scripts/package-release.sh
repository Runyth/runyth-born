#!/usr/bin/env bash
set -euo pipefail

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r version="$(tr -d '[:space:]' < "${project_root}/VERSION")"
declare -r release_dir="${project_root}/release"
declare -r temporary_root="$(mktemp -d)"
declare -r source_name="runyth-born-${version}-src"
declare -r runtime_name="runyth-born-${version}-run"
declare -r docs_name="runyth-born-${version}-doc"
trap 'rm -rf -- "${temporary_root}"' EXIT

cd -- "${project_root}"

# Release provenance: package exactly what a committed revision contains and
# refuse extra workspace files or uncommitted changes.
if [[ -n "$(git status --porcelain)" ]]; then
  printf 'Refusing to package: the workspace has uncommitted or untracked changes\n' >&2
  git status --porcelain >&2
  exit 1
fi
declare -r revision="$(git rev-parse HEAD)"

"${script_dir}/scan-source-history.sh"

cd -- "${project_root}"
rm -rf -- "${release_dir}"
mkdir -p -- "${release_dir}"

# Produce the committed tree for every bundle: untracked and ignored files can
# never泄漏 into the Source, Runtime, or Documentation archives (F12), and the
# staged files are verified byte for byte and mode for mode against the commit
# below, so hidden index state such as assume-unchanged cannot substitute
# altered content (F12).
cp -a -- "${project_root}" "${temporary_root}/${source_name}"
rm -rf -- "${temporary_root}/${source_name}/dist" "${temporary_root}/${source_name}/release" "${temporary_root}/${source_name}/node_modules"

(
  cd -- "${temporary_root}/${source_name}"
  git clean -xdfq
  rm -rf -- .git/logs .git/COMMIT_EDITMSG .git/MERGE_MSG .git/ORIG_HEAD .git/FETCH_HEAD .git/gc.log .git/gc.pid
)
python3 - "${temporary_root}/${source_name}/.git/config" <<'PY'
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
if path.exists():
    text = path.read_text(encoding="utf-8")
    scrubbed = re.sub(r"(://[^/@\s]+):[^/@\s]+@", r"\1@", text)
    if scrubbed != text:
        path.write_text(scrubbed, encoding="utf-8")
PY

# Verify the staged tree against the committed revision: every tracked file
# must have the committed bytes and the committed mode (F12).
(
  cd -- "${temporary_root}/${source_name}"
  declare -r staged_dir="${PWD}"
  bash scripts/verify-committed-tree.sh "${staged_dir}" "${project_root}" || exit 1
)

declare -r dist_dir="${temporary_root}/dist"
SITE_BASE_PATH="${SITE_BASE_PATH:-}" bash "${temporary_root}/${source_name}/scripts/build-static.sh" "${dist_dir}"
python3 "${temporary_root}/${source_name}/scripts/validate-static.py" "${dist_dir}"

mkdir -p -- "${temporary_root}/${runtime_name}" "${temporary_root}/${docs_name}"
cp -a -- "${dist_dir}/." "${temporary_root}/${runtime_name}/"

for documentation_file in \
  README.md \
  CHANGELOG.md \
  OPERATIONS.md \
  SECURITY.md \
  TEMPLATE_FORMAT.md \
  AI_TEMPLATE_PROMPT.md \
  RELEASE_VALIDATION.md \
  LICENSE; do
  cp -- "${temporary_root}/${source_name}/${documentation_file}" "${temporary_root}/${docs_name}/${documentation_file}"
done
cp -a -- "${temporary_root}/${source_name}/docs" "${temporary_root}/${docs_name}/docs"

(
  cd -- "${temporary_root}"
  zip -q -r "${release_dir}/${source_name}.zip" "${source_name}"
  zip -q -r "${release_dir}/${runtime_name}.zip" "${runtime_name}"
  zip -q -r "${release_dir}/${docs_name}.zip" "${docs_name}"
)

# Verify the published archives after creation: integrity, inventories,
# shipped bytes, and no server, Go, or WebAssembly assets in the runtime
# bundle (F12).
python3 - "${release_dir}" "${version}" "${revision}" "${dist_dir}" "${temporary_root}/${source_name}" "${temporary_root}/${docs_name}" <<'PY'
import hashlib
import sys
import zipfile
from pathlib import Path

release_dir = Path(sys.argv[1])
version = sys.argv[2]
revision = sys.argv[3]
dist_dir = Path(sys.argv[4])
source_root = Path(sys.argv[5])
docs_staging = Path(sys.argv[6])

archives = sorted(release_dir.glob("*.zip"))
if len(archives) != 3:
    raise SystemExit(f"expected exactly three release archives, found {len(archives)}")

archives_info = {}
for archive in archives:
    with zipfile.ZipFile(archive) as bundle:
        bad = bundle.testzip()
        if bad is not None:
            raise SystemExit(f"{archive.name}: corrupt entry {bad}")
        archives_info[archive.name] = {info.filename: info for info in bundle.infolist()}

names = {name: set(infos) for name, infos in archives_info.items()}
files = {
    name: set(
        filename for filename in filenames
        if not archives_info[name][filename].is_dir()
    )
    for name, filenames in names.items()
}

src_name = next(name for name in files if name.endswith("-src.zip"))
run_name = next(name for name in files if name.endswith("-run.zip"))
doc_name = next(name for name in files if name.endswith("-doc.zip"))
run_root = run_name[:-len(".zip")]
doc_root = doc_name[:-len(".zip")]


def file_digest(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


# Every runtime entry must match the built dist/ byte for byte (F12).
published = sorted(name[len(run_root) + 1:] for name in files[run_name])
dist_entries = set()
for path in sorted(dist_dir.rglob("*")):
    if path.is_file():
        dist_entries.add(path.relative_to(dist_dir).as_posix())
if published != sorted(dist_entries):
    raise SystemExit("runtime archive does not match the built dist/ contents")
with zipfile.ZipFile(release_dir / run_name) as bundle:
    for name in published:
        archived = hashlib.sha256(bundle.read(f"{run_root}/{name}")).hexdigest()
        if archived != file_digest(dist_dir / name):
            raise SystemExit(f"runtime archive entry differs from dist/: {name}")

for forbidden in ["Dockerfile", "compose.yaml", "wasm_exec.js"]:
    if any(path.endswith(forbidden) for path in published):
        raise SystemExit("runtime archive unexpectedly contains server or WASM assets")
for suffix in [".go", ".wasm", ".exe"]:
    if any(path.endswith(suffix) for path in published):
        raise SystemExit("runtime archive unexpectedly contains executable assets")

for required in ["license.txt", "ai-template-prompt.txt", "templates/catalog.json"]:
    if required not in published:
        raise SystemExit(f"runtime archive is missing {required}")


# Every documentation entry must match the committed documentation bytes and
# the exact committed documentation inventory (F12).
expected_documents = {"LICENSE", "README.md", "CHANGELOG.md", "OPERATIONS.md",
                      "SECURITY.md", "TEMPLATE_FORMAT.md", "AI_TEMPLATE_PROMPT.md",
                      "RELEASE_VALIDATION.md"}
documentation_inventory = set()
for path in sorted(docs_staging.rglob("*")):
    if path.is_file():
        documentation_inventory.add(path.relative_to(docs_staging).as_posix())
published_docs = sorted(name[len(doc_root) + 1:] for name in files[doc_name])
if published_docs != sorted(documentation_inventory):
    raise SystemExit("documentation archive does not match the committed documentation inventory")
with zipfile.ZipFile(release_dir / doc_name) as bundle:
    for name in published_docs:
        archived = hashlib.sha256(bundle.read(f"{doc_root}/{name}")).hexdigest()
        if archived != file_digest(docs_staging / name):
            raise SystemExit(f"documentation archive entry differs from the committed file: {name}")

for required in ["LICENSE", "README.md", "CHANGELOG.md", "docs"]:
    if not any(
        name == f"{doc_root}/{required}" or name.startswith(f"{doc_root}/{required}/")
        for name in files[doc_name]
    ):
        raise SystemExit(f"{doc_name} is missing {required}")

chunks = [
    "# Runyth Born release provenance",
    f"# version {version}",
    f"# revision {revision}",
    "# verify with: sha256sum -c checksums-sha256.txt",
]
lines = "\n".join(chunks) + "\n\n"
checksum_path = release_dir / "checksums-sha256.txt"
with open(checksum_path, "w", encoding="utf-8") as checksum_file:
    checksum_file.write(lines)
    for archive in archives:
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        checksum_file.write(f"{digest}  {archive.name}\n")

print(f"Release verification passed: {len(archives)} archives at revision {revision}")
PY

# Verify the actual published Source ZIP content: extract it, check the Git
# object integrity, compare the shipped project files against the committed
# tree byte for byte and mode for mode, scan every published object and Git
# file with redacted diagnostics, and confirm the configuration carries no
# embedded credentials (F12).
declare -r verification_root="$(mktemp -d)"
trap 'rm -rf -- "${temporary_root}" "${verification_root}"' EXIT
python3 - "${release_dir}/${source_name}.zip" "${verification_root}" <<'PY'
import os
import sys
import zipfile
from pathlib import Path

archive_path = Path(sys.argv[1])
target = Path(sys.argv[2])
with zipfile.ZipFile(archive_path) as bundle:
    for info in bundle.infolist():
        bundle.extract(info, target)
    # The shipped file modes are part of the archive; extraction must restore
    # them so the committed-mode verification sees the exact published file
    # set (F12).
    for info in bundle.infolist():
        if info.is_dir():
            continue
        mode = (info.external_attr >> 16) & 0o777
        if mode:
            os.chmod(target / info.filename, mode)
PY
(
  cd -- "${verification_root}/${source_name}"
  git fsck --no-progress --full --strict
  declared="$(git ls-files | sort)"
  shipped="$(find . -type f ! -path './.git/*' | sed 's|^./||' | sort)"
  test "${declared}" = "${shipped}" || {
    diff <(printf '%s\n' "${declared}") <(printf '%s\n' "${shipped}") >&2
    printf 'The shipped Source ZIP inventory does not match the committed tree\n' >&2
    exit 1
  }
  test "$(git -C . rev-parse HEAD)" = "${revision}" || {
    printf 'The archived Git revision does not match the packaged revision\n' >&2
    exit 1
  }
  if grep -E -q '(://[^/@[:space:]]+):[^/@[:space:]]+@' .git/config; then
    printf 'The archived Git configuration still contains credential-bearing URLs\n' >&2
    exit 1
  fi
  git config --get-regexp '^url\.' >/dev/null 2>&1 || true
  bash scripts/verify-committed-tree.sh . || exit 1
  bash scripts/scan-source-history.sh
)
printf 'Source ZIP verification passed: committed tree, integrity, inventory, and privacy\n'

printf 'Created release artifacts:\n'
printf '  %s\n' "${release_dir}/${source_name}.zip"
printf '  %s\n' "${release_dir}/${runtime_name}.zip"
printf '  %s\n' "${release_dir}/${docs_name}.zip"
printf '  %s\n' "${release_dir}/checksums-sha256.txt"
