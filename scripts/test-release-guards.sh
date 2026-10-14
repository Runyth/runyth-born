#!/usr/bin/env bash
set -uo pipefail

# Integrated release-guard canaries (F14): each case builds a disposable
# clone, plants one fictional canary, and runs the actual packaging. Negative
# cases must fail packaging WITH evidence that the intended guard rejected
# their canary; path errors, build failures, or unrelated failures fail the
# test instead of counting as guard success. The ignored-inputs case must
# succeed with no fixture bytes anywhere in the published bundles. Fixture
# identity is configured locally, so no maintainer Git configuration is
# required. Usage: scripts/test-release-guards.sh

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
declare -r project_root="$(cd -- "${script_dir}/.." && pwd -P)"

declare -r workroot="$(realpath -m -- "$(mktemp -d "${TMPDIR:-/tmp}/runyth-born-release-guards-XXXXXXXX")")"
trap 'rm -rf -- "${workroot}"' EXIT

# Synthetic canary values are assembled at runtime so the guard script never
# itself contains a token matching the scanner patterns.
CANARY="ghs_$(printf 'a%.0s' $(seq 1 40))"
failures=0

abortSetup() {
  printf 'guard setup failed: %s — the test cannot continue\n' "${1}" >&2
  exit 2
}

freshClone() {
  declare -r name="${1}"
  declare -r target="${workroot}/${name}"
  if ! git clone --quiet --no-hardlinks -- "${project_root}" "${target}"; then
    abortSetup "cannot clone ${name}"
  fi
  # Fixtures run under an ephemeral local identity so no host configuration,
  # signing, or maintainer setting is required.
  git -C "${target}" config user.name "Release Guard Battery"
  git -C "${target}" config user.email "release-guard-battery@localhost"
  # The battery exercises the current packaging pipeline: overlay the working
  # scripts into the clone and commit them only when they actually differ from
  # the committed versions (a clean clone needs no commit at all), and commit
  # failures must abort rather than corrupt the case path. The overlay carries
  # the clone's committed modes, not the copying filesystem's.
  cp -- "${project_root}/scripts/package-release.sh" \
        "${project_root}/scripts/scan-source-history.sh" \
        "${project_root}/scripts/build-static.sh" \
        "${project_root}/scripts/verify-committed-tree.sh" \
        "${project_root}/scripts/check-release-tag.sh" \
        "${project_root}/scripts/validate-static.py" \
        "${target}/scripts/"
  (
    cd -- "${target}" || abortSetup "cannot prepare ${name}"
    git ls-files -s scripts |
      while IFS=$'\t' read -r entry; do
        mode="${entry%%$'\t'*}"
        path="${entry#*$'\t'}"
        chmod "${mode:3:3}" "${path}"
      done
    if [[ -n "$(git status --porcelain)" ]]; then
      git add scripts
      if ! git commit -q -m "canary battery pipeline overlay"; then
        abortSetup "overlay commit failed in ${name}"
      fi
    fi
  )
  mkdir -p -- "${target}/.git/info" || abortSetup "cannot prepare ${name}"
  printf 'fixture.log\n' > "${target}/.git/info/exclude"
  printf '%s' "${target}"
}

# Runs packaging in a clone expecting failure and confirms the exact guard
# evidence line the case expects. Returns 0 only when the intended guard
# rejected the canary in its output; any unrelated refusal, path error, or
# silent success returns 1.
expectFail() {
  declare -r name="${1}"
  declare -r target="${2}"
  declare -r evidence="${3}"
  local output status=0 matched
  output="$(cd -- "${target}" && bash scripts/package-release.sh 2>&1)" || status=$?
  if [[ "${status}" -eq 0 ]]; then
    printf 'guard %s: packaging unexpectedly succeeded\n' "${name}" >&2
    return 1
  fi
  matched="$(printf '%s\n' "${output}" | grep -E "${evidence}" | head -1 || true)"
  if [[ -z "${matched}" ]]; then
    printf 'guard %s: failed without its intended guard evidence (%s)\n' "${name}" "${evidence}" >&2
    return 1
  fi
  printf 'guard %s: rejected packaging (%s)\n' "${name}" "${matched}"
  return 0
}

caseTracked() {
  local target
  target="$(freshClone tracked)"
  printf 'script %s\n' "${CANARY}" > "${target}/tests/fixture.js"
  git -C "${target}" add tests/fixture.js
  git -C "${target}" commit -q -m "add fixture" || abortSetup "tracked fixture commit failed"
  expectFail tracked-content "${target}" 'Credential-like' || failures=1
}

caseMessage() {
  local target
  target="$(freshClone message)"
  git -C "${target}" commit -q --allow-empty -m "note with ${CANARY}" || abortSetup "message fixture commit failed"
  expectFail commit-message "${target}" 'Credential-like' || failures=1
}

caseTag() {
  local target
  target="$(freshClone tag)"
  git -C "${target}" tag -a -m "tagged ${CANARY}" guarded-tag HEAD || abortSetup "tag fixture failed"
  expectFail tag-message "${target}" 'Credential-like' || failures=1
}

caseBinary() {
  local target
  target="$(freshClone binary)"
  printf 'prefix\xff\x00canary %s' "${CANARY}" > "${target}/tests/fixture.bin"
  git -C "${target}" add tests/fixture.bin
  git -C "${target}" commit -q -m "add binary fixture" || abortSetup "binary fixture commit failed"
  expectFail binary-blob "${target}" 'Credential-like' || failures=1
}

caseDangling() {
  local target
  target="$(freshClone dangling)"
  printf 'dangling %s\n' "${CANARY}" | git -C "${target}" hash-object -w --stdin >/dev/null
  expectFail dangling-blob "${target}" 'Credential-like' || failures=1
}

caseAuthor() {
  local target
  target="$(freshClone author)"
  git -C "${target}" -c user.name="author canary ${CANARY}" commit -q --allow-empty -m "clean message" || abortSetup "author fixture commit failed"
  expectFail commit-author-line "${target}" 'Credential-like' || failures=1
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
  expectFail assume-unchanged-bytes "${target}" 'does not match the committed revision|tracked bytes changed' || failures=1
}

# An authenticated remote-style URL in a tracked file must be recognized by
# the decoded object scan despite the engines' divergent whitespace syntax
# (F12). The URL is assembled at runtime so the battery's own script can never
# satisfy the pattern.
caseAuthenticatedURL() {
  local target
  target="$(freshClone url)"
  printf 'plan %s\n' "$(printf '%s://%s@%s/%s' 'https' 'build-user:fictional-pass' 'release-host.invalid' 'checkout')" > "${target}/tests/fixture.md"
  git -C "${target}" add tests/fixture.md
  git -C "${target}" commit -q -m "add url fixture" || abortSetup "url fixture commit failed"
  expectFail authenticated-http-url "${target}" 'Credential-like' || failures=1
}

# Removing the owner execute bit while hiding the mode change from ordinary
# status must be caught by the committed executable-class verification (F12).
caseOwnerMode() {
  local target
  target="$(freshClone owner-mode)"
  chmod 0654 "${target}/scripts/check.sh"
  git -C "${target}" config core.filemode false
  if [[ -n "$(git -C "${target}" status --porcelain)" ]]; then
    printf 'guard owner-mode: the fixture is not hidden in this Git version\n' >&2
    failures=1
    return 0
  fi
  expectFail owner-exec-mode "${target}" 'does not match the committed revision|tracked executable bits changed' || failures=1
}

# A filesystem with uniform modes on every tracked file must still be checked
# where the chmod probe round-trips (F12); the class error must surface.
caseUniformMode() {
  local target
  target="$(freshClone uniform-mode)"
  (
    cd -- "${target}" || abortSetup "cannot prepare uniform-mode"
    git ls-files -z | xargs -0 chmod u+x
  )
  git -C "${target}" config core.filemode false
  if [[ -n "$(git -C "${target}" status --porcelain)" ]]; then
    printf 'guard uniform-mode: the fixture is not hidden in this Git version\n' >&2
    failures=1
    return 0
  fi
  expectFail uniform-mode-class "${target}" 'does not match the committed revision|tracked executable bits changed' || failures=1
}

caseRequiredFailingSetup() {
  # Negative control: path and setup failures must fail the test rather than
  # count as guard success. A deliberately broken fixture directory produces
  # neither evidence nor a release, so expectFail must report it as a test
  # failure instead of "rejected packaging".
  if expectFail broken-setup "${workroot}/db704b62-5eb0-4a3d-bb11-a64d0b1f145c" 'Credential-like|Refusing|does not match|changed'; then
    printf 'guard negative-control: broken setup wrongly counted as a guard rejection\n' >&2
    failures=1
  fi
}

# Ignored site/ and docs/ inputs must vanish from Runtime and Documentation
# while ordinary packaging keeps succeeding (F12).
caseIgnoredInputs() {
  local target
  target="$(freshClone ignored-inputs)"
  printf 'run-ignore %s\n' "${CANARY}" > "${target}/site/fixture.log"
  printf 'doc-ignore %s\n' "${CANARY}" > "${target}/docs/fixture.log"
  local output status=0
  output="$(cd -- "${target}" && bash scripts/package-release.sh 2>&1)" || status=$?
  if [[ "${status}" -ne 0 ]]; then
    printf 'guard ignored-inputs: packaging failed (%s)\n' "$(printf '%s\n' "${output}" | grep -E 'Refusing|Error|error' | head -1)" >&2
    failures=1
    return 0
  fi
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
  failures=1
}

# Packaging invoked through an absolute alias of the checkout must stage a real
# isolated copy: the original's ignored files, node_modules markers and Git
# logs survive both a successful package and an in-pipeline failure (F15).
caseAliasPackaging() {
  local target alias
  target="$(freshClone alias)"
  printf 'developer private ignored content\n' > "${target}/developer-fixture.log"
  mkdir -p "${target}/node_modules/probe"
  printf 'ignored node_modules marker\n' > "${target}/node_modules/probe/marker.log"
  alias="${workroot}/absolute-alias"
  if ! ln -s -- "${target}" "${alias}"; then
    abortSetup "cannot create the alias fixture"
  fi
  originalPreserved() {
    test -f "${target}/developer-fixture.log" &&
      test -f "${target}/node_modules/probe/marker.log" &&
      test -s "${target}/.git/logs/HEAD" &&
      [[ -z "$(git -C "${target}" status --porcelain)" ]]
  }
  if ! bash "${alias}/scripts/package-release.sh" >/dev/null 2>&1; then
    printf 'guard alias-packaging: successful packaging through the alias failed\n' >&2
    failures=1
    return 0
  fi
  if ! originalPreserved; then
    printf 'guard alias-packaging: the original checkout lost ignored files, node_modules markers, or Git logs\n' >&2
    failures=1
    return 0
  fi
  # A packaging failure that begins after staging cleanup must also leave the
  # original untouched.
  if SITE_BASE_PATH='bad/../value' bash "${alias}/scripts/package-release.sh" >/dev/null 2>&1; then
    printf 'guard alias-packaging: the broken packaging unexpectedly succeeded\n' >&2
    failures=1
    return 0
  fi
  if ! originalPreserved; then
    printf 'guard alias-packaging: the failed packaging damaged the original checkout\n' >&2
    failures=1
    return 0
  fi
  printf 'guard alias-packaging: staged cleanup isolated the original checkout through success and failure\n'
}

caseTracked
caseMessage
caseTag
caseBinary
caseDangling
caseAuthor
caseHiddenIndex
caseAuthenticatedURL
caseOwnerMode
caseUniformMode
caseAliasPackaging
caseRequiredFailingSetup
caseIgnoredInputs

if [[ "${failures}" -eq 0 ]]; then
  printf 'Release guard canaries passed: every fixture behaves as required\n'
fi
exit "${failures}"
