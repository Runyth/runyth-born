#!/usr/bin/env bash
set -euo pipefail

# Representative credential and private-data scan over the content a packaged
# Source ZIP publishes: every blob reachable from the current refs, every
# commit and annotated tag message, and the plain files inside .git such as
# the copied configuration and packed refs (F12). Matches are reported
# redacted — classification and location only, never the matched text.
# Invalid patterns fail loudly instead of silently scanning nothing.
# Usage: scripts/scan-source-history.sh [repository-root]

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"
declare -r scan_root="${1:-"${project_root}"}"

patterns=(
  'GitHub personal token|ghp_[A-Za-z0-9]{36,40}'
  'GitHub fine-grained token|github_pat_[A-Za-z0-9_]{60,100}'
  'GitHub OAuth token|gho_[A-Za-z0-9]{36,40}'
  'GitHub server token|ghs_[A-Za-z0-9]{36,40}'
  'AWS access key|AKIA[0-9A-Z]{16}'
  'OpenAI-style API key|sk-[A-Za-z0-9_-]{32,}'
  'Google API key|AIza[0-9A-Za-z_-]{35}'
  'Private key header|-----BEGIN (RSA |EC |OPENSSH |DSA |PGP |ENCRYPTED )?PRIVATE KEY-----'
  'HTTP URL credentials|https?://[^/[:space:]@]+:[^/[:space:]@]+@'
  'Authorization header|Authorization: *(Basic |Bearer )[A-Za-z0-9+/=._-]{8,}'
)

failures=0
report() {
  printf 'Credential-like content detected (%s) in %s\n' "$1" "$2" >&2
  failures=1
}

combined=""
classes=()
regexes=()
for pattern in "${patterns[@]}"; do
  class="${pattern%%|*}"
  regex="${pattern#*|}"
  probe_status=0
  printf 'scan-pattern-probe' | grep -E -- "${regex}" >/dev/null 2>&1 || probe_status=$?
  if [[ "${probe_status}" -ge 2 ]]; then
    printf 'Invalid scan pattern for %s\n' "${class}" >&2
    exit 1
  fi
  classes+=("${class}")
  regexes+=("${regex}")
  if [[ -z "${combined}" ]]; then
    combined="${regex}"
  else
    combined="${combined}|${regex}"
  fi
done

# Trees and blobs of every reachable commit (-I skips binary objects), with
# the commit messages of the same revisions.
commits=()
while IFS= read -r commit; do
  commits+=("${commit}")
done < <(git -C "${scan_root}" rev-list --all 2>/dev/null)
for commit in "${commits[@]}"; do
  if matches="$(git -C "${scan_root}" grep -I -h -c -E -- "${combined}" "${commit}" -- 2>/dev/null)" && [[ -n "${matches}" ]]; then
    index=0
    for regex in "${regexes[@]}"; do
      if hit="$(git -C "${scan_root}" grep -I -h -c -E -- "${regex}" "${commit}" -- 2>/dev/null)" && [[ -n "${hit}" ]]; then
        while IFS= read -r line; do
          report "${classes[${index}]}" "tracked content (${line})"
        done <<< "${hit}"
      fi
      index=$((index + 1))
    done
  fi
  if git -C "${scan_root}" log -1 --format=%B "${commit}" 2>/dev/null | grep -E -q -- "${combined}"; then
    index=0
    for regex in "${regexes[@]}"; do
      if git -C "${scan_root}" log -1 --format=%B "${commit}" 2>/dev/null | grep -E -q -- "${regex}"; then
        report "${classes[${index}]}" "commit message ${commit}"
      fi
      index=$((index + 1))
    done
  fi
done

# Annotated tag messages.
while IFS=$'\t' read -r refname objecttype objectname; do
  [[ "${objecttype}" == "tag" ]] || continue
  if git -C "${scan_root}" cat-file tag "${objectname}" 2>/dev/null | sed '1,/^$/d' | grep -E -q -- "${combined}"; then
    index=0
    for regex in "${regexes[@]}"; do
      if git -C "${scan_root}" cat-file tag "${objectname}" 2>/dev/null | sed '1,/^$/d' | grep -E -q -- "${regex}"; then
        report "${classes[${index}]}" "tag message ${refname}"
      fi
      index=$((index + 1))
    done
  fi
done < <(git -C "${scan_root}" for-each-ref --format='%(refname)%09%(objecttype)%09%(objectname)' refs/tags/ 2>/dev/null)

# Plain Git support files copied into the archive; binary objects are skipped.
if file_paths="$(grep -R -I -l -E -- "${combined}" "${scan_root}/.git" 2>/dev/null)" && [[ -n "${file_paths}" ]]; then
  while IFS= read -r file_path; do
    index=0
    for regex in "${regexes[@]}"; do
      if grep -I -q -E -- "${regex}" "${file_path}" 2>/dev/null; then
        report "${classes[${index}]}" "git file ${file_path#"${scan_root}"/}"
      fi
      index=$((index + 1))
    done
  done <<< "${file_paths}"
fi

if [[ "${failures}" -eq 0 ]]; then
  printf 'History scan passed: no credential-like content in published objects\n'
fi
exit "${failures}"
