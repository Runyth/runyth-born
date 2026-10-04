#!/usr/bin/env bash
set -euo pipefail

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r temporary_dist="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dist}"' EXIT

cd -- "${project_root}"

declare -r unformatted_files="$(gofmt -l .)"
if [[ -n "${unformatted_files}" ]]; then
  printf 'The following Go files need gofmt:\n%s\n' "${unformatted_files}" >&2
  exit 1
fi

go test -race ./...
go vet ./...

while IFS= read -r -d '' javascript_file; do
  node --check "${javascript_file}"
done < <(find site/assets -type f -name '*.js' -print0 | sort -z)

node --test tests/js/*.test.mjs
"${script_dir}/build-static.sh" "${temporary_dist}"
node tests/js/static-smoke.mjs "${temporary_dist}"
python3 "${script_dir}/validate-static.py" "${temporary_dist}"
