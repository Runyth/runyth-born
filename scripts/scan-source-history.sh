#!/usr/bin/env bash
set -euo pipefail

# Representative credential and private-data scan over every object reachable
# from the current refs. Packaged Source ZIPs ship the full history, so the
# release gate refuses to package when a pattern matches.
# Usage: scripts/scan-source-history.sh

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd)"

patterns=(
  'ghp_'"[A-Za-z0-9]{36,40}"
  'github_pat_'"[A-Za-z0-9_]{60,100}"
  'gho_'"[A-Za-z0-9]{36}"
  'ghs_'"[A-Za-z0-9]{36}"
  'AKIA'"[0-9A-Z]{16}"
  'sk-[A-Za-z0-9'"_-]{32,}"
  'AIza'"[0-9A-Za-z_-]{35}"
  '-----BEGIN (RSA |EC |OPENSSH |DSA |PGP |ENCRYPTED )?PRIVATE KEY-----'
  'https?://'"[^/[:space:]@\"']+:[^/[:space:]@\"']+@"
  'Authorization: *(Basic|Basic |Bearer|Bearer ) *[A-Za-z0-9+/=._-]{8,}'
)

matches=""
for pattern in "${patterns[@]}"; do
  while IFS= read -r commit; do
    if found="$(git -C "${project_root}" grep -I -h -n -E "${pattern}" "${commit}" 2>/dev/null)" && [[ -n "${found}" ]]; then
      printf 'Credential-like content in reachable history (%s):\n%s\n' "${commit}" "${found}" >&2
      matches="found"
    fi
  done < <(git -C "${project_root}" rev-list --all)
done

if [[ -z "${matches}" ]]; then
  printf 'History scan passed: no credential-like content in reachable objects\n'
  exit 0
fi
exit 1
