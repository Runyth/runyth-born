#!/usr/bin/env bash
set -euo pipefail

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r version="$(tr -d '[:space:]' < "${project_root}/VERSION")"
declare -r release_dir="${project_root}/release"
declare -r dist_dir="${project_root}/dist"
declare -r temporary_root="$(mktemp -d)"
declare -r source_name="template-wizard-web-${version}-src"
declare -r runtime_name="template-wizard-web-${version}-run"
declare -r docs_name="template-wizard-web-${version}-doc"
trap 'rm -rf -- "${temporary_root}"' EXIT

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

printf 'Created release artifacts:\n'
printf '  %s\n' "${release_dir}/${source_name}.zip"
printf '  %s\n' "${release_dir}/${runtime_name}.zip"
printf '  %s\n' "${release_dir}/${docs_name}.zip"
