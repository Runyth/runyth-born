# Changelog

## Unreleased

- Establish save authority across both storage copies: a stale tab can no longer tie or bury a newer acknowledged fallback save after a primary-store recovery, tied divergent copies load with the other preserved for review, preserved recovery records stay visible from both stores, legacy fallback sessions migrate without IndexedDB with the fresher legacy copy selected, and deleting a migrated questionnaire also removes the legacy copies so it cannot return.
- Preserve every divergent portable-import branch regardless of its save counter, with review plus an import choice for imported copies instead of silent rejection.
- Avoid false save conflicts in a single tab: queued snapshots carry the acknowledged revision at execution time, and a rejected edit's preservation is awaited and surfaced.
- Escape preserved-answer serialization with the same literal-text contract as ordinary exported Markdown, so entity strings, heading markers, and raw HTML cannot change meaning in recovery copies.
- Complete block scalar semantics: keep-chomping, blank-only content, blank runs next to more-indented lines, first-line indentation detection, and explicit rejection of malformed headers.
- Reject build output directories that overlap tracked source for symlinked checkouts as well, by canonicalizing both project roots physically.
- Verify the published Source ZIP itself: packaging ships exactly the committed tree (ignored and untracked files excluded), drops local navigation metadata, scrubs credential URLs from the copied Git configuration, and validates the extracted archive's integrity, inventory, revision, and privacy with redacted scanning over Git objects, commit messages, tag messages, and Git support files.
- Install locked development dependencies before the quality gates so the Markdown fidelity regression never silently skips.
- Rename the project to Runyth Born (`runyth-born` for identifiers) across the Go module, npm checks package, browser storage database and fallback keys, release and workflow artifact names, site branding, and documentation; sessions saved under the previous storage names are migrated automatically on first open, with the legacy originals kept.
- Guard acknowledged saves with monotonic revisions enforced through read-compare-update storage transactions: stale tabs can no longer overwrite newer answers by navigating, conflicting edits are rejected with a clear status and preserved recoverably, and navigation positions persist separately from answers.
- Preserve unmatched saved answers during template reconciliation instead of discarding them; reconciliation now reports exact, fallback, and unmatched counts, and a notice with a review dialog offers copy and discard actions over the preserved copy.
- Order divergent copies by save revisions instead of wall-clock timestamps, so clock skew or rollback can no longer restore an older snapshot over a newer acknowledged save; fallback storage keeps the newest acknowledged save and portable snapshots carry their revision.
- Escape ampersands in literal answer and template text so HTML-entity strings such as `&copy;` stay literal in exported Markdown, validated by a real renderer.
- Correct YAML block scalar handling: explicit indentation indicators, folded blank-line and more-indented semantics, comments after block scalar headers, and plain scalars containing apostrophes; the supported subset is documented.
- Reject build output directories that overlap tracked source by validating destinations against the tracked inventory instead of a hardcoded directory list.
- Enforce agreement between a release tag, the packaged commit, and `VERSION` before publication, with matching and mismatching regression coverage.
- Pin the build toolchain to the supported Go 1.26.9 release line while keeping the language minimum at 1.23.
- Pin all workflow actions to full commit SHAs and separate read-only release building from write-capable publication, which receives only the validated artifacts.
- Record release provenance and verify archives: packaging refuses dirty workspaces, scans reachable history for credential-like content, verifies archive inventories and integrity, and publishes SHA-256 checksums for every release.
- Publish the licence text with the deployed site, add a confirmed "Delete local copy" control in the finish dialog, document browser-origin trust and storage lifecycle limits, and refresh stale audit and repository documentation.
- Remove browser Go/WebAssembly execution; compile templates to JSON at build time and use plain JavaScript for reconciliation and Markdown downloads.
- Confirm IndexedDB saves only after transaction commit and fall back after an abort.
- Preserve cleared answers and restrict fallback merging to globally unique labels.
- Fix touch section navigation, save error handling, invalid-session controls, completion edits, and current-question relocation after template updates.
- Bound resume-link decompression and preserve import fragments until successful reconciliation and saving.
- Reject trailing JSON, malformed inline YAML JSON, and sequence merge keys; remove a YAML input-triggered panic.
- Exclude questions from unsupported contexts and escape answer Markdown punctuation.
- Generate deployment-aware custom 404 links, escape template URLs, support uppercase template extensions, and protect source directories from build output cleanup.
- Add artifact validation, regression tests, and Chromium acceptance at root and project URLs.

## 2.0.0 - 2026-09-11

### Breaking

- Replace the entire server-backed application architecture with a completely static site intended for GitHub Pages and other static hosts.
- Remove the Go HTTP server, REST session API, server-side session files, Docker/Compose/Caddy/SWAG deployment stacks, health endpoints, environment-based runtime configuration, and backup/restore scripts.
- Store autosave state in the browser instead of on the server; legacy 1.x `/w/<token>` sessions are not directly resumable after migration.
- Require a rebuild/redeployment when repository templates change instead of dynamically rescanning a server directory.

### Added

- Compile the existing mdast/YAML/Markdown engine to Go WebAssembly so all template analysis, answer reconciliation, and Markdown rendering happen client-side.
- Add IndexedDB autosave with a `localStorage` compatibility fallback.
- Add portable cross-device resume links that encode a compressed questionnaire snapshot entirely in the URL fragment.
- Add a generated static template catalog and repository-relative asset routing compatible with GitHub Pages project subpaths.
- Add dedicated GitHub Actions workflows for CI, GitHub Pages deployment, and tagged release packaging.
- Add static artifact validation and JavaScript/WASM smoke coverage.

### Changed

- Make `master` the release/deployment branch in CI and Pages workflows.
- Keep the existing guided questionnaire UI, progress counters, navigation, completion dialog, Markdown download, template reconciliation, JSON/YAML support, and 65,536-character answer limit while moving those behaviours fully into the browser.
- Change copied resume URLs from stable server tokens to portable snapshot links; the normal local wizard URL remains a random browser-local session identifier.

## 1.1.4 - 2026-08-08

### Fixed

- Update each section's answered counter live from the current answer state instead of rendering the stale count captured when the questionnaire first loaded.
- Remove duplicate chapter numbering in the sidebar and question header when the template heading already contains its matching number, such as `1. Core`.

### Added

- Show the current question position within the active section alongside the existing overall question position.
- Show a gentle completion dialog after finishing, with a clear reminder to send the downloaded Markdown file to the Dungeon Master and a private resume-link field for future reference or re-editing.
- Add a prominent centered copy-success toast that animates downward after the resume link is copied from the completion dialog.
- Turn the final question's **Next** control into the same green **Finish and download** action used by the finish panel.

## 1.1.3 - 2026-08-08

### Fixed

- Keep the answer field and Previous/Next controls at stable positions when moving between questions of different lengths.
- Give the guidance/context region and active question panel fixed heights, with internal scrolling for unusually long content instead of allowing those regions to push the answer controls down.
- Keep the answer textarea at a stable height and disable manual vertical resizing so the navigation controls remain anchored during a questionnaire session.
- Keep long section labels on one line with ellipsis so section-title length cannot shift the questionnaire layout.

## 1.1.2 - 2026-08-08

### Fixed

- Make questionnaire section navigation respond on the first mouse click even while the answer textarea is focused.
- Preserve keyboard and non-mouse activation while avoiding the textarea blur/save race that previously made sidebar navigation feel like it required two clicks.

## 1.1.1 - 2026-08-08

- Fixed local Windows and localhost startup flows that could reject **Start this template** with `Cross-site form submission rejected.`.
- Removed the same-origin gate from anonymous session creation, where no existing user state is mutated and the check could produce false positives through localhost aliases, reverse proxies, or local privacy/security software.
- Kept same-origin validation and per-session CSRF-token validation unchanged for all existing-session mutations such as answer, position, and finish writes.
- Added regression tests proving cross-origin session creation succeeds while cross-origin mutation of an existing session remains forbidden even with a valid CSRF token.

## 1.1.0 - 2026-07-25

- Split production deployment into two explicit modes.
- Changed the default `compose.yaml` to a slim existing-proxy deployment with no published host ports and no TLS or ACME components.
- Added configurable attachment to an existing external Docker proxy network through `TW_PROXY_NETWORK`.
- Added the stable `TW_PROXY_HOSTNAME` network alias for SWAG and other containerised proxies.
- Added a SWAG nginx proxy configuration sample with preserved Host headers and disabled access logging for private resume-token paths.
- Kept `compose.caddy.yaml` as the standalone automatic-HTTPS deployment that owns host ports 80 and 443.
- Isolated the application from public networking in Caddy mode through an internal backend network.
- Made restore operations deployment-aware through `TW_COMPOSE_FILE`.
- Expanded deployment, operations, and security documentation for both modes.

## 1.0.0 - 2026-07-25

- Added a self-hosted browser edition of Template Wizard.
- Reused the generic mdast JSON/YAML parser, question scanner, answer merger, and canonical Markdown renderer.
- Added responsive section and question navigation.
- Added per-answer autosave with atomic JSON and Markdown replacement.
- Added private 256-bit resume URLs and separate CSRF tokens.
- Added browser-local recent-session shortcuts.
- Added current and final Markdown downloads.
- Added dynamic template discovery without image rebuilds or restarts.
- Added template-version answer migration.
- Added embedded AI template-authoring prompt.
- Added health and readiness endpoints.
- Added hardened scratch-based Docker image.
- Added localhost-only and automatic-HTTPS Docker Compose deployments.
- Added backup, restore, validation, and cross-platform build scripts.
- Added GitHub Actions CI and release workflows using Node.js 24-compatible action versions and disabled Go dependency caching for the standard-library-only module.
