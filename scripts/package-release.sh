#!/usr/bin/env bash
set -euo pipefail

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r version="$(tr -d '[:space:]' < "${project_root}/VERSION")"
declare -r release_dir="${project_root}/release"
declare -r dist_dir="${project_root}/dist"
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

"${script_dir}/build-static.sh" "${dist_dir}"
python3 "${script_dir}/validate-static.py" "${dist_dir}"

cp -a -- "${project_root}" "${temporary_root}/${source_name}"
rm -rf -- "${temporary_root}/${source_name}/dist" "${temporary_root}/${source_name}/release" "${temporary_root}/${source_name}/node_modules"

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
  cp -- "${project_root}/${documentation_file}" "${temporary_root}/${docs_name}/${documentation_file}"
done
cp -a -- "${project_root}/docs" "${temporary_root}/${docs_name}/docs"

(
  cd -- "${temporary_root}"
  zip -q -r "${release_dir}/${source_name}.zip" "${source_name}"
  zip -q -r "${release_dir}/${runtime_name}.zip" "${runtime_name}"
  zip -q -r "${release_dir}/${docs_name}.zip" "${docs_name}"
)

# Verify the published archives after creation: integrity, inventories, and no
# server, Go, or WebAssembly assets in the runtime bundle.
python3 - "${release_dir}" "${version}" "${revision}" "${dist_dir}" <<'PY'
import hashlib
import sys
import zipfile
from pathlib import Path

release_dir = Path(sys.argv[1])
version = sys.argv[2]
revision = sys.argv[3]
dist_dir = Path(sys.argv[4])

archives = sorted(release_dir.glob("*.zip"))
if len(archives) != 3:
    raise SystemExit(f"expected exactly three release archives, found {len(archives)}")

names = {}
for archive in archives:
    with zipfile.ZipFile(archive) as bundle:
        bad = bundle.testzip()
        if bad is not None:
            raise SystemExit(f"{archive.name}: corrupt entry {bad}")
        names[archive.name] = set(
            info.filename for info in bundle.infolist() if not info.is_dir()
        )

src_name = next(name for name in names if name.endswith("-src.zip"))
run_name = next(name for name in names if name.endswith("-run.zip"))
doc_name = next(name for name in names if name.endswith("-doc.zip"))
src_root = src_name[:-len(".zip")]
run_root = run_name[:-len(".zip")]
doc_root = doc_name[:-len(".zip")]

for required in ["README.md", "LICENSE", ".git/HEAD"]:
    if f"{src_root}/{required}" not in names[src_name]:
        raise SystemExit(f"{src_name} is missing {required}")

expected = sorted(
    path.relative_to(dist_dir).as_posix() for path in dist_dir.rglob("*") if path.is_file()
)
published = sorted(
    name[len(run_root) + 1:] for name in names[run_name]
)
if published != expected:
    raise SystemExit("runtime archive does not match the built dist/ contents")

for forbidden in ["Dockerfile", "compose.yaml", "wasm_exec.js"]:
    if any(path.endswith(forbidden) for path in published):
        raise SystemExit("runtime archive unexpectedly contains server or WASM assets")
for suffix in [".go", ".wasm", ".exe"]:
    if any(path.endswith(suffix) for path in published):
        raise SystemExit("runtime archive unexpectedly contains executable assets")

for required in ["license.txt", "ai-template-prompt.txt", "templates/catalog.json"]:
    if required not in published:
        raise SystemExit(f"runtime archive is missing {required}")

for required in ["LICENSE", "README.md", "CHANGELOG.md", "docs"]:
    if not any(
        path == f"{doc_root}/{required}" or path.startswith(f"{doc_root}/{required}/")
        for path in names[doc_name]
    ):
        raise SystemExit(f"{doc_name} is missing {required}")

lines = [
    "# Runyth Born release provenance",
    f"# version {version}",
    f"# revision {revision}",
    "# verify with: sha256sum -c checksums-sha256.txt",
]
checksum_path = release_dir / "checksums-sha256.txt"
with open(checksum_path, "w", encoding="utf-8") as checksum_file:
    checksum_file.write("\n".join(lines) + "\n\n")
    for archive in archives:
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        checksum_file.write(f"{digest}  {archive.name}\n")

print(f"Release verification passed: {len(archives)} archives at revision {revision}")
PY

printf 'Created release artifacts:\n'
printf '  %s\n' "${release_dir}/${source_name}.zip"
printf '  %s\n' "${release_dir}/${runtime_name}.zip"
printf '  %s\n' "${release_dir}/${docs_name}.zip"
printf '  %s\n' "${release_dir}/checksums-sha256.txt"
