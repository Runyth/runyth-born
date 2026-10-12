# Release Validation

## Required checks

Run:

```bash
make check
```

The gate verifies:

- `gofmt` cleanliness;
- `go test -race ./...`;
- `go vet ./...`;
- JavaScript syntax;
- Node.js unit tests for save revisions, conflict rejection, legacy migration, session/resume encoding, build-output guards, release-tag agreement, and Markdown fidelity;
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

Chromium checks root and project paths, local reload/resume, touch navigation, cleared answers, literal Markdown downloads, completion, portable import in a fresh browser context, localStorage fallback, missing sessions, and nested 404 recovery. It also covers stale-tab protection (navigation and divergent edits cannot overwrite newer acknowledged answers), revision-ordered imports under clock skew, legacy `template-wizard-web` session migration from both the legacy database and legacy fallback keys with originals kept, and the confirmed finish-dialog deletion of a local questionnaire. Runtime requests must be GETs for same-origin static files. CI runs these checks before changes can pass validation.

## Release packaging

Run:

```bash
make package
```

Packaging refuses a workspace with uncommitted or untracked changes, scans every reachable Git object for representative credential and private-content patterns, and verifies the archives after creation (integrity, source inventory, runtime archive matching the built `dist/`, documentation inventory). The release workflow additionally refuses a tag that does not exactly match `v${VERSION}` or that does not resolve to the packaged commit.

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
