# Release Validation

## Required checks

The locked development dependencies (`npm ci`) must be installed before the
gates: the Markdown fidelity regression requires its renderer and fails loudly
when it is unavailable instead of silently reducing coverage.

Run:

```bash
npm ci
make check
```

The gate verifies:

- `gofmt` cleanliness;
- `go test -race ./...`;
- `go vet ./...`;
- JavaScript syntax;
- Node.js unit tests for shared save authority across both storage copies, recovery visibility, fallback-only migration, deletion across migration boundaries, divergent-branch imports, queued save conflicts, session/resume encoding, build-output guards including symlinked checkouts, release-tag agreement, and Markdown fidelity;
- a complete static build;
- plain JavaScript reconciliation/rendering against every published template;
- generated template catalog integrity;
- HTML local-asset references and the published licence text;
- absence of Go/Docker/server deployment files from the runtime artifact;
- absence of WebAssembly and `wasm_exec.js`;
- generated answer-slot integrity and local JavaScript module imports.

## Automated browser acceptance

```bash
npm ci
npx playwright install chromium
make browser-check
```

Chromium checks root and project paths, local reload/resume, touch navigation, cleared answers, literal Markdown downloads, completion, portable import in a fresh browser context, localStorage fallback, missing sessions, and nested 404 recovery. It also covers stale-tab protection (navigation and divergent edits cannot overwrite newer acknowledged answers), cross-store authority (a stale primary write is rejected while a newer acknowledged fallback save exists), revision-ordered imports under clock skew with divergent branches preserved in every direction and reviewable through the import choice, serialized single-tab saves without false conflicts, legacy `template-wizard-web` session migration from both the legacy database and legacy fallback keys with originals kept, and deletion of migrated questionnaires that also removes the legacy copies so they cannot return. Runtime requests must be GETs for same-origin static files. CI runs these checks before changes can pass validation.

## Release packaging

Run:

```bash
npm ci
make package
```

Packaging refuses a workspace with uncommitted or untracked changes, scans every reachable Git object, commit message, and annotated tag message for representative credential and private-content patterns with redacted diagnostics, and verifies the archives after creation: the source archive is extracted to confirm Git object integrity, a project inventory exactly matching the committed tree (ignored and untracked workspace files are never shipped), the packaged revision, credential-free Git configuration, and redacted privacy scanning; the runtime archive must match the built `dist/`, and the documentation inventory is checked. The release workflow additionally refuses a tag that does not exactly match `v${VERSION}` or that does not resolve to the packaged commit.

Expected artifacts:

```text
release/runyth-born-<version>-src.zip
release/runyth-born-<version>-run.zip
release/runyth-born-<version>-doc.zip
release/checksums-sha256.txt
```

The source ZIP intentionally contains `.git` and its available repository history. The runtime ZIP contains only deployable static-site files plus the published licence text. The documentation ZIP contains no runtime binaries. Verify downloaded archives with `sha256sum -c checksums-sha256.txt`.

## Manual browser acceptance

For a final interactive acceptance pass:

1. `make serve`.
2. Start the bundled template.
3. Enter answers containing multiple lines and Markdown punctuation.
4. Move between questions and sections; confirm section/overall counters update immediately.
5. Reload; confirm local autosave resumes.
6. Copy the resume link into a different browser profile; confirm it imports the questionnaire.
7. Download the current Markdown and verify answers are placed after their bold labels.
8. Finish on the final question; confirm the Markdown download and completion dialog.
