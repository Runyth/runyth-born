#!/usr/bin/env bash
set -uo pipefail

# Integrated release-guard canaries (F12): each case builds a disposable
# clone, plants one fictional canary, and runs the actual packaging. Negative
# cases must fail packaging with redacted diagnostics; the ignored-inputs case
# must succeed with no fixture bytes anywhere in the published bundles.
# Usage: scripts/test-release-guards.sh

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"

declare -r basename_root="$(mktemp -d "${TMPDIR:-/tmp}/runyth-born-release-guards-XXXXXXXX")"
declare -r workroot="$(realpath -m -- "${basename_root}")"
trap 'rm -rf -- "${workroot}"' EXIT

# Synthetic canary values are assembled at runtime so the guard script never
# itself contains a token matching the scanner patterns.
CANARY="ghs_$(printf 'a%.0s' $(seq 1 40))"
failures=0

freshClone() {
  declare -r name="${1}"
  declare -r target="${workroot}/${name}"
  git clone --quiet --no-hardlinks -- "${project_root}" "${target}"
  # The battery exercises the CURRENT packaging pipeline: overlay the working
  # scripts into the clone and commit them so packaging sees a clean tree.
  cp -p -- "${project_root}/scripts/package-release.sh" \
        "${project_root}/scripts/scan-source-history.sh" \
        "${project_root}/scripts/build-static.sh" \
        "${project_root}/scripts/verify-committed-tree.sh" \
        "${project_root}/scripts/check-release-tag.sh" \
        "${project_root}/scripts/validate-static.py" \
        "${target}/scripts/"
  git -C "${target}" add scripts
  git -C "${target}" -c user.name=battery -c user.email=battery@localhost commit -qm "canary battery pipeline overlay"
  mkdir -p -- "${target}/.git/info"
  printf 'fixture.log\n' > "${target}/.git/info/exclude"
  printf '%s' "${target}"
}

# Runs packaging in a clone expecting failure; prints the redacted one-line
# classification of the first reason on failure.
expectFail() {
  declare -r name="${1}"
  declare -r target="${2}"
  if (cd -- "${target}" && bash scripts/package-release.sh >/dev/null 2>&1); then
    printf 'guard %s: packaging unexpectedly succeeded\n' "${name}" >&2
    failures=1
    return 0
  fi
  declare reason
  reason="$(cd -- "${target}" && bash scripts/package-release.sh 2>&1 |
    grep -E 'Credential-like|Refusing|does not match|changed' | sort -u | head -1 || true)"
  printf 'guard %s: rejected packaging (%s)\n' "${name}" "${reason:-failed}"
}

caseTracked() {
  local target
  target="$(freshClone tracked)"
  printf 'script %s\n' "${CANARY}" > "${target}/tests/fixture.js"
  git -C "${target}" add tests/fixture.js
  git -C "${target}" -c user.name=x -c user.email=x@x commit -qm "add fixture"
  expectFail tracked-content "${target}"
}

caseMessage() {
  local target
  target="$(freshClone message)"
  git -C "${target}" -c user.name=x -c user.email=x@x commit -q --allow-empty -m "note with ${CANARY}"
  expectFail commit-message "${target}"
}

caseTag() {
  local target
  target="$(freshClone tag)"
  git -C "${target}" tag -a -m "tagged ${CANARY}" guarded-tag HEAD
  expectFail tag-message "${target}"
}

caseBinary() {
  local target
  target="$(freshClone binary)"
  printf 'prefix\xff\x00canary %s' "${CANARY}" > "${target}/tests/fixture.bin"
  git -C "${target}" add tests/fixture.bin
  git -C "${target}" -c user.name=x -c user.email=x@x commit -qm "add binary fixture"
  expectFail binary-blob "${target}"
}

caseDangling() {
  local target
  target="$(freshClone dangling)"
  printf 'dangling %s\n' "${CANARY}" | git -C "${target}" hash-object -w --stdin >/dev/null
  expectFail dangling-blob "${target}"
}

caseAuthor() {
  local target
  target="$(freshClone author)"
  git -C "${target}" -c user.name="author canary ${CANARY}" -c user.email=x@x commit -q --allow-empty -m "clean message"
  expectFail commit-author-line "${target}"
}

caseHiddenIndex() {
  local target
  target="$(freshClone assume-unchanged)"
  printf 'assume-unchanged %s\n' "${CANARY}" >> "${target}/README.md"
  git -C "${target}" update-index --assume-unchanged README.md
  if [[ -n "$(git -C "${target}" status --porcelain)" ]]; then
    printf 'guard assume-unchanged: the fixture is not hidden in this Git version\n' >&2
    failures=1
    return 0
  fi
  expectFail assume-unchanged-bytes "${target}"
}

# Ignored site/ and docs/ inputs must vanish from Runtime and Documentation
# while ordinary packaging keeps succeeding (F12).
caseIgnoredInputs() {
  local target
  target="$(freshClone ignored-inputs)"
  printf 'run-ignore %s\n' "${CANARY}" > "${target}/site/fixture.log"
  printf 'doc-ignore %s\n' "${CANARY}" > "${target}/docs/fixture.log"
  if (cd -- "${target}" && bash scripts/package-release.sh >/dev/null 2>&1); then
    declare leaked=0
    while IFS= read -r artifact; do
      case "${artifact}" in *-src.zip) continue;; esac
      if unzip -p "${artifact}" '*/fixture.log' 2>/dev/null | grep -q .; then
        printf 'guard ignored-inputs: %s still contains the fixture\n' "${artifact}" >&2
        leaked=1
      fi
    done < <(find "${target}/release" -name '*.zip' | sort)
    if [[ "${leaked}" -eq 0 ]]; then
      printf 'guard ignored-inputs: packaging succeeded and shipped no fixtures\n'
      return 0
    fi
  else
    printf 'guard ignored-inputs: packaging failed\n' >&2
  fi
  failures=1
  return 0
}

caseTracked
caseMessage
caseTag
caseBinary
caseDangling
caseAuthor
caseHiddenIndex
caseIgnoredInputs

if [[ "${failures}" -eq 0 ]]; then
  printf 'Release guard canaries passed: every fixture behaves as required\n'
fi
exit "${failures}"
