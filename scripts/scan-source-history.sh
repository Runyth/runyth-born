#!/usr/bin/env bash
set -euo pipefail

# Representative credential and private-data scan over every piece of content
# the packaged Source ZIP publishes: all stored Git objects — reachable or
# not, binary or text — decoded through cat-file so commit headers, tag
# bodies, tree entries and binary payloads are scanned as their real bytes,
# plus the plain files inside .git such as the copied configuration and
# packed refs (F12). Matches are reported redacted — classification, object
# identity and location only, never the matched text.
# Patterns carry %PLACEHOLDER% skeletons because POSIX ERE and Python regular
# expressions do not share whitespace syntax; each engine receives its own
# validated translation, and a fictional positive control must match there
# for every classification before any scanning starts (F12). Invalid
# patterns or controls fail loudly instead of silently scanning nothing.
# Usage: scripts/scan-source-history.sh [repository-root]

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"
declare -r scan_root="${1:-"${project_root}"}"

patterns=(
  'GitHub personal token|ghp_%TOKEN38%'
  'GitHub fine-grained token|github_pat_%TOKEN60%'
  'GitHub OAuth token|gho_%TOKEN38%'
  'GitHub server token|ghs_%TOKEN38%'
  'AWS access key|AKIA%KEY16%'
  'OpenAI-style API key|sk-%TOKEN32%'
  'Google API key|AIza%KEY35%'
  'Private key header|-----BEGIN %KEYWORD%PRIVATE KEY-----'
  'HTTP URL credentials|https?://[^/%SPACE%@]+:[^/%SPACE%@]+@'
  'Authorization header|Authorization: *(Basic |Bearer )[A-Za-z0-9+/=._-]{8,}'
)

declare -r filler64="$(printf 'a%.0s' $(seq 1 64))"

# Fictional positive controls per class, assembled at runtime so the
# scanner's own source never contains fully-formed matches (F12).
controlFor() {
  local -r class="${1}"
  case "${class}" in
    'GitHub personal token') printf 'ghp_%s' "${filler64:0:36}" ;;
    'GitHub fine-grained token') printf 'github_pat_%s' "${filler64:0:60}" ;;
    'GitHub OAuth token') printf 'gho_%s' "${filler64:0:36}" ;;
    'GitHub server token') printf 'ghs_%s' "${filler64:0:36}" ;;
    'AWS access key') printf 'AKIA%s' "$(printf 'Z%.0s' $(seq 1 16))" ;;
    'OpenAI-style API key') printf 'sk-%s' "${filler64:0:32}" ;;
    'Google API key') printf 'AIza%s' "${filler64:0:35}" ;;
    # The key word exists only at runtime: the skeleton in this file carries a
    # placeholder, so the file itself cannot satisfy the pattern.
    'Private key header') printf -- '-----BEGIN %s PRIVATE KEY-----' 'EC' ;;
    # The URL and scheme parts are assembled at runtime, so no source line of
    # this scanner itself can satisfy the pattern (F12).
    'HTTP URL credentials') printf '%s://%s@%s/%s' 'http' 'user:fictional-pass' 'scanner-control.invalid' 'archive' ;;
    'Authorization header') printf 'Authorization: Basic %s' "${filler64:0:12}==" ;;
  esac
}

# Expands the pattern skeletons into engine-local ERE forms, keeping the
# original pattern shapes exactly. The %SPACE% placeholder resolves to the
# POSIX bracket-expression class element ([:space:]) for grep and to \s for
# the Python object scanner: the two engines parse those whitespace
# conventions differently, so each engine receives its own validated form
# plus a matching control (F12).
expandCommon() {
  local -r skeleton="${1}"
  local -r token38='[A-Za-z0-9]{36,40}'
  local -r token60='[A-Za-z0-9_]{60,100}'
  local -r token32='[A-Za-z0-9_-]{32,}'
  local -r key16='[0-9A-Z]{16}'
  local -r key35='[0-9A-Za-z_-]{35}'
  local -r keyword='(RSA |EC |OPENSSH |DSA |PGP |ENCRYPTED )?'
  local expanded="${skeleton//%TOKEN38%/${token38}}"
       expanded="${expanded//%TOKEN60%/${token60}}"
       expanded="${expanded//%TOKEN32%/${token32}}"
       expanded="${expanded//%KEY16%/${key16}}"
       expanded="${expanded//%KEY35%/${key35}}"
       expanded="${expanded//%KEYWORD%/${keyword}}"
  printf '%s' "${expanded}"
}

posixForm() {
  printf '%s' "${1//%SPACE%/[:space:]}"
}

pythonForm() {
  local -r replacement='\s'
  printf '%s' "${1//%SPACE%/${replacement}}"
}

declare -a classes=()
declare -a posix_patterns=()
declare -a python_patterns=()
combined=""
for pattern in "${patterns[@]}"; do
  class="${pattern%%|*}"
  skeleton="${pattern#*|}"
  common="$(expandCommon "${skeleton}")"
  posix="$(posixForm "${common}")"
  python_pattern="$(pythonForm "${common}")"
  control="$(controlFor "${class}")"
  probe_status=0
  printf 'scan-pattern-probe' | grep -E -- "${posix}" >/dev/null 2>&1 || probe_status=$?
  if [[ "${probe_status}" -ge 2 ]]; then
    printf 'Invalid scan pattern for %s\n' "${class}" >&2
    exit 1
  fi
  if ! printf '%s' "${control}" | grep -E -q -- "${posix}"; then
    printf 'Scan pattern for %s does not match its positive control (grep engine)\n' "${class}" >&2
    exit 1
  fi
  # The object scanner runs under Python re, whose whitespace syntax differs
  # from POSIX ERE; the Python engine must see both a valid expression and a
  # matching control or the scanner refuses to run (F12).
  if ! python3 - "${class}" "${python_pattern}" "${control}" <<'PY'
import re
import sys

python_pattern = sys.argv[2].encode()
re.compile(python_pattern)
control = sys.argv[3].encode()
if not re.search(python_pattern, control):
    sys.exit(1)
PY
  then
    printf 'Scan pattern for %s does not match its positive control (object scanner engine)\n' "${class}" >&2
    exit 1
  fi
  classes+=("${class}")
  posix_patterns+=("${posix}")
  python_patterns+=("${python_pattern}")
  if [[ -z "${combined}" ]]; then
    combined="${posix}"
  else
    combined="${combined}|${posix}"
  fi
done
probe_status=0
printf 'scan-pattern-probe' | grep -E -- "${combined}" >/dev/null 2>&1 || probe_status=$?
if [[ "${probe_status}" -ge 2 ]]; then
  printf 'Invalid combined scan pattern\n' >&2
  exit 1
fi

failures=0
report() {
  printf 'Credential-like content detected (%s) in %s\n' "$1" "$2" >&2
  failures=1
}

# Decode every stored object of the repository. This is exactly the object set
# a packaged Source ZIP ships, so dangling and out-of-ref objects are part of
# the published bytes and are scanned the same way (F12).
python3 - "${scan_root}" "${classes[@]}" "${python_patterns[@]}" <<'PY'
import re
import subprocess
import sys

root = sys.argv[1]
count = (len(sys.argv) - 2) // 2
specs = []
for index in range(count):
    specs.append((sys.argv[2 + index], re.compile(sys.argv[2 + count + index].encode(), re.DOTALL)))

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
    stream.read(1)
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
    for posix in "${posix_patterns[@]}"; do
      if grep -I -q -E -- "${posix}" "${file_path}" 2>/dev/null; then
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
