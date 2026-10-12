#!/usr/bin/env bash
set -euo pipefail

# Build the complete deployable static site. No server executable is produced.
declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r output_dir="${1:-${project_root}/dist}"
declare -r version="$(tr -d '[:space:]' < "${project_root}/VERSION")"

cd -- "${project_root}"

# Never delete the project directory, one of its ancestors, or tracked source
# when a caller supplies an output override. Only the default dist directory is
# a designated output inside the repository.
declare -r resolved_output="$(realpath -m -- "${output_dir}")"
if [[ "${resolved_output}" == "${project_root}" || "${project_root}/" == "${resolved_output}/"* ]]; then
  printf 'Refusing to replace the project directory or an ancestor: %s\n' "${output_dir}" >&2
  exit 1
fi
if [[ "${resolved_output}/" == "${project_root}/.git/"* ]]; then
  printf 'Refusing to replace the Git repository: %s\n' "${output_dir}" >&2
  exit 1
fi
if [[ "${resolved_output}/" == "${project_root}/"* && "${resolved_output}" != "${project_root}/dist" ]]; then
  declare -r relative_output="${resolved_output#"${project_root}"/}"
  if git -C "${project_root}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if [[ -n "$(git -C "${project_root}" ls-files -- "${relative_output}")" ]] ||
      git -C "${project_root}" ls-files | cut -d/ -f1 | sort -u | grep -qxF -- "${relative_output%%/*}"; then
      printf 'Refusing to replace tracked source: %s\n' "${output_dir}" >&2
      exit 1
    fi
  else
    printf 'Refusing an in-repository output override without Git: %s\n' "${output_dir}" >&2
    exit 1
  fi
fi

rm -rf -- "${output_dir}"
mkdir -p -- "${output_dir}/templates" "${output_dir}/assets"
cp -a -- "${project_root}/site/." "${output_dir}/"
cp -- "${project_root}/AI_TEMPLATE_PROMPT.md" "${output_dir}/ai-template-prompt.txt"
cp -- "${project_root}/LICENSE" "${output_dir}/license.txt"
cp -- "${project_root}/VERSION" "${output_dir}/VERSION"

# GitHub serves 404.html at the missing URL, including nested paths. Configure
# its base explicitly; ordinary pages continue using portable relative URLs.
python3 - "${output_dir}/404.html" "${SITE_BASE_PATH:-}" <<'PY'
import html
import sys
from pathlib import Path
base = sys.argv[2].strip('/')
if any(part in {'.', '..'} for part in base.split('/')) or any(char in base for char in '?#\\'):
    raise SystemExit('Invalid SITE_BASE_PATH')
href = '/' + base + '/' if base else '/'
path = Path(sys.argv[1])
path.write_text(path.read_text().replace('<base href="/">', f'<base href="{html.escape(href, quote=True)}">'))
PY

go run ./cmd/template-catalog \
  -templates "${project_root}/templates" \
  -output "${output_dir}/templates/catalog.json" \
  -version "${version}"

while IFS= read -r -d '' template_file; do
  cp -- "${template_file}" "${output_dir}/templates/"
done < <(find "${project_root}/templates" -maxdepth 1 -type f \
  \( -iname '*.json' -o -iname '*.yaml' -o -iname '*.yml' \) -print0 | sort -z)

python3 "${script_dir}/validate-static.py" "${output_dir}"
printf 'Built static site %s at %s\n' "${version}" "${output_dir}"
