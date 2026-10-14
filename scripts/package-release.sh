#!/usr/bin/env bash
set -euo pipefail

# The roots are canonicalized physically so invoking this script through an
# absolute checkout alias cannot smuggle the alias path into staging (F15);
# packaging always stages a real isolated copy of the physical checkout.
declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"
declare -r version="$(tr -d '[:space:]' < "${project_root}/VERSION")"
declare -r release_dir="${project_root}/release"
declare -r temporary_root="$(realpath -m -- "$(mktemp -d)")"
declare -r source_name="runyth-born-${version}-src"
declare -r runtime_name="runyth-born-${version}-run"
declare -r docs_name="runyth-born-${version}-doc"
declare -r staged_source="${temporary_root}/${source_name}"
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
# never leak into the Source, Runtime, or Documentation archives (F12), and the
# staged files are verified byte for byte and mode for mode against the commit
# below, so hidden index state such as assume-unchanged cannot substitute
# altered content (F12).
#
# The staged copy must be a real isolated directory under the temporary root,
# never a symlink to the original checkout: the destructive cleanup below must
# only ever touch the staging copy (F15). A nested .git directory symlink is a
# valid Git layout that cp -a preserves; -d follows it, so cleanup would then
# delete the original reflogs. Replace that link with a physical copy and
# prove the copy stays inside staging before any metadata removal.
mkdir -p -- "${staged_source}"
cp -a -- "${project_root}/." "${staged_source}/"
if [[ -L "${staged_source}/.git" ]]; then
  if [[ ! -d "${staged_source}/.git" ]]; then
    printf 'The staged Git directory is a symlink and is not an isolated directory\n' >&2
    exit 1
  fi
  resolved_git="$(cd -- "${staged_source}/.git" && pwd -P)"
  rm -- "${staged_source}/.git"
  mkdir -p -- "${staged_source}/.git"
  cp -a -- "${resolved_git}/." "${staged_source}/.git/"
fi
if [[ -L "${staged_source}" || -L "${staged_source}/.git" || ! -d "${staged_source}" || ! -d "${staged_source}/.git" ]]; then
  printf 'The staged source tree is not an isolated physical directory\n' >&2
  exit 1
fi
declare -r staged_physical="$(cd -- "${staged_source}" && pwd -P)"
declare -r staged_git_physical="$(cd -- "${staged_source}/.git" && pwd -P)"
if [[ "${staged_physical}/" != "${temporary_root}/"* ]] || [[ "${staged_physical}" == "${project_root}/"* ]] || [[ "${staged_physical}" == "${project_root}" ]]; then
  printf 'The staged source tree is not isolated from the packaging root\n' >&2
  exit 1
fi
if [[ "${staged_git_physical}/" != "${staged_physical}/"* ]]; then
  printf 'The staged Git directory is not isolated from the original checkout\n' >&2
  exit 1
fi
rm -rf -- "${staged_source}/dist" "${staged_source}/release" "${staged_source}/node_modules"

# A copied core.worktree can name the original checkout. Git honors that
# setting over the shell's current directory, so cleanup must select and
# prove its working tree before deleting anything. Only staged configuration
# files are rewritten; the invoking checkout's configuration is never opened
# for writing (F15).
sanitize_staged_worktree_config() {
  local config parent
  if [[ -L "${staged_source}/.git/config" || ! -f "${staged_source}/.git/config" ]]; then
    printf 'The staged Git configuration is not an isolated file\n' >&2
    exit 1
  fi
  while IFS= read -r -d '' config; do
    if [[ -L "${config}" ]]; then
      printf 'The staged Git configuration is not an isolated file\n' >&2
      exit 1
    fi
    parent="$(cd -- "$(dirname -- "${config}")" && pwd -P)"
    if [[ "${parent}" != "${staged_git_physical}" && "${parent}/" != "${staged_git_physical}/"* ]]; then
      printf 'The staged Git configuration is not an isolated file\n' >&2
      exit 1
    fi
    if git config --file "${config}" --get-all core.worktree >/dev/null 2>&1; then
      git config --file "${config}" --unset-all core.worktree
    fi
  done < <(find -P "${staged_source}/.git" -type f \( -name config -o -name config.worktree \) -print0)
}

require_staged_worktree() {
  local toplevel gitdir toplevel_physical gitdir_physical
  toplevel="$(env -u GIT_DIR -u GIT_WORK_TREE git --git-dir="${staged_git_physical}" --work-tree="${staged_physical}" rev-parse --show-toplevel)"
  toplevel_physical="$(cd -- "${toplevel}" && pwd -P)"
  if [[ "${toplevel_physical}" != "${staged_physical}" ]]; then
    printf 'Refusing to clean a Git working tree outside the staged copy\n' >&2
    exit 1
  fi
  gitdir="$(env -u GIT_DIR -u GIT_WORK_TREE git --git-dir="${staged_git_physical}" --work-tree="${staged_physical}" rev-parse --absolute-git-dir)"
  gitdir_physical="$(cd -- "${gitdir}" && pwd -P)"
  if [[ "${gitdir_physical}" != "${staged_git_physical}" ]]; then
    printf 'Refusing to clean a Git directory outside the staged copy\n' >&2
    exit 1
  fi
  toplevel="$(env -u GIT_DIR -u GIT_WORK_TREE git -C "${staged_physical}" rev-parse --show-toplevel)"
  toplevel_physical="$(cd -- "${toplevel}" && pwd -P)"
  if [[ "${toplevel_physical}" != "${staged_physical}" ]]; then
    printf 'Refusing to clean a Git working tree outside the staged copy\n' >&2
    exit 1
  fi
}

# Removes Git metadata only when the path is a real entry inside the physical
# staged Git directory. A symlink is unlinked and never followed (F15).
remove_isolated_git_metadata() {
  local git_dir="${1}"
  local git_physical entry path physical parent
  if [[ -L "${git_dir}" || ! -d "${git_dir}" ]]; then
    printf 'Refusing to clean Git metadata outside the staged copy\n' >&2
    exit 1
  fi
  git_physical="$(cd -- "${git_dir}" && pwd -P)"
  if [[ "${git_physical}" != "${staged_git_physical}" ]]; then
    printf 'Refusing to clean Git metadata outside the staged copy\n' >&2
    exit 1
  fi
  for entry in logs COMMIT_EDITMSG MERGE_MSG ORIG_HEAD FETCH_HEAD gc.log gc.pid; do
    path="${git_dir}/${entry}"
    if [[ -L "${path}" ]]; then
      rm -- "${path}"
      continue
    fi
    if [[ -d "${path}" ]]; then
      physical="$(cd -- "${path}" && pwd -P)"
      if [[ "${physical}/" != "${git_physical}/"* ]]; then
        printf 'Refusing to clean Git metadata outside the staged copy\n' >&2
        exit 1
      fi
      rm -rf -- "${path}"
      continue
    fi
    if [[ -e "${path}" ]]; then
      parent="$(cd -- "$(dirname -- "${path}")" && pwd -P)"
      if [[ "${parent}" != "${git_physical}" ]]; then
        printf 'Refusing to clean Git metadata outside the staged copy\n' >&2
        exit 1
      fi
      rm -f -- "${path}"
    fi
  done
}

sanitize_staged_worktree_config
require_staged_worktree
env -u GIT_DIR -u GIT_WORK_TREE \
  git --git-dir="${staged_git_physical}" --work-tree="${staged_physical}" \
  clean -xdfq
remove_isolated_git_metadata "${staged_source}/.git"
if [[ -L "${staged_source}/.git/config" ]]; then
  printf 'The staged Git configuration is not an isolated file\n' >&2
  exit 1
fi
python3 - "${staged_source}/.git/config" <<'PY'
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
  cd -- "${staged_source}"
  declare -r staged_dir="${PWD}"
  bash scripts/verify-committed-tree.sh "${staged_dir}" "${project_root}" || exit 1
)

declare -r dist_dir="${temporary_root}/dist"
SITE_BASE_PATH="${SITE_BASE_PATH:-}" bash "${staged_source}/scripts/build-static.sh" "${dist_dir}"
python3 "${staged_source}/scripts/validate-static.py" "${dist_dir}"

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
  cp -- "${staged_source}/${documentation_file}" "${temporary_root}/${docs_name}/${documentation_file}"
done
cp -a -- "${staged_source}/docs" "${temporary_root}/${docs_name}/docs"

(
  cd -- "${temporary_root}"
  zip -q -r "${release_dir}/${source_name}.zip" "${source_name}"
  zip -q -r "${release_dir}/${runtime_name}.zip" "${runtime_name}"
  zip -q -r "${release_dir}/${docs_name}.zip" "${docs_name}"
)

# Verify the published archives after creation: integrity, inventories,
# shipped bytes, and no server, Go, or WebAssembly assets in the runtime
# bundle (F12).
python3 - "${release_dir}" "${version}" "${revision}" "${dist_dir}" "${staged_source}" "${temporary_root}/${docs_name}" <<'PY'
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
