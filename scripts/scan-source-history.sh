#!/usr/bin/env bash
set -euo pipefail

# Representative credential and private-data scan over every piece of content
# the packaged Source ZIP publishes: all stored Git objects — reachable or
# not, binary or text — decoded through cat-file so commit headers, tag
# bodies, tree entries and binary payloads are scanned as their real bytes,
# plus the plain files inside .git such as the copied configuration and
# packed refs (F12). Matches are reported redacted — classification,
# object identity and location only, never the matched text. Invalid patterns
# fail loudly instead of silently scanning nothing.
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

declare -a classes=()
declare -a regexes=()
combined=""
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

failures=0
report() {
  printf 'Credential-like content detected (%s) in %s\n' "$1" "$2" >&2
  failures=1
}

# Decode every stored object of the repository. This is exactly the object set
# a packaged Source ZIP ships, so dangling and out-of-ref objects are part of
# the published bytes and are scanned the same way (F12).
python3 - "${scan_root}" "${classes[@]}" "${regexes[@]}" <<'PY'
import re
import subprocess
import sys

root = sys.argv[1]
count = (len(sys.argv) - 2) // 2
specs = [(sys.argv[2 + i], re.compile(sys.argv[2 + count + i].encode(), re.DOTALL)) for i in range(count)]
combined = [spec[1] for spec in specs]

found = False


def report(cls, location):
    global found
    found = True
    print(f"Credential-like content detected ({cls}) in {location}", file=sys.stderr)


batch = subprocess.Popen(
    ["git", "-C", root, "cat-file", "--batch", "--batch-all-objects", "--buffer"],
    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
stream = batch.stdout
while True:
    header = stream.readline()
    if not header:
        break
    sha, kind, size = header.decode().split()
    body = b""
    remaining = int(size)
    while remaining > 0:
        chunk = stream.read(min(remaining, 1 << 20))
        if not chunk:
            break
        body += chunk
        remaining -= len(chunk)
    trailing_break = stream.read(1)
    for cls, pattern in specs:
        if pattern.search(body):
            report(cls, f"git object {sha} ({kind})")
batch.wait()
if found:
    sys.exit(1)
PY
object_status=$?
if [[ "${object_status}" -ne 0 ]]; then
  exit "${object_status}"
fi

# Plain Git support files copied into the archive. Their bytes are outside the
# object database and are scanned as files.
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
