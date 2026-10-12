#!/usr/bin/env bash
set -euo pipefail

# Refuse a tagged release whose trigger does not exactly match the declared
# version, so published assets always agree with the tag naming them (F05).
# Usage: check-release-tag.sh <tag-ref-name> [declared-version]

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"
declare -r tag="${1:-}"
declare -r version="${2:-$(tr -d '[:space:]' < "${project_root}/VERSION")}"

if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
  printf 'Refusing a release triggered by a non-version ref: %s\n' "${tag}" >&2
  exit 1
fi
if [[ "${tag}" != "v${version}" ]]; then
  printf 'Refusing a release whose tag %s does not match the declared version %s\n' "${tag}" "${version}" >&2
  exit 1
fi
