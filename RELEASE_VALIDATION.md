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
- Node.js unit tests for shared save authority across both storage copies, tied-copy preservation of real question answers, recovery records from both stores with prior-schema upgrades, merged clock-rollback branches and atomic concurrent writes, fallback-only migration, deletion across migration boundaries, tombstoned identifiers that block queued saves, recovery writes, and re-migration, durable deletion markers under fallback quota pressure, divergent-branch imports, queued save conflicts, session/resume encoding, build-output guards including symlinked checkouts, release-tag agreement, block scalar semantics including quoted hashes inside inline JSON, and Markdown fidelity;
- a complete static build;
- plain JavaScript reconciliation/rendering against every published template;
- generated template catalog integrity;
- HTML local-asset references and the published licence text;
- absence of Go/Docker/server deployment files from the runtime artifact;
- absence of WebAssembly and `wasm_exec.js`;
- generated answer-slot integrity and local JavaScript module imports;
- release-guard canaries (`scripts/test-release-guards.sh`), which run the real packaging in disposable clones against fictional canaries for tracked content, commit messages, tag messages, binary blobs, dangling stored blobs, author headers, hidden index tricks, and ignored bundle inputs.

## Automated browser acceptance

```bash
npm ci
npx playwright install chromium
make browser-check
```

Chromium checks root and project paths, local reload/resume, touch navigation, cleared answers, literal Markdown downloads, completion, portable import in a fresh browser context, localStorage fallback, missing sessions, and nested 404 recovery. It also covers stale-tab protection (navigation and divergent edits cannot overwrite newer acknowledged answers), cross-store authority (a stale primary write is rejected while a newer acknowledged fallback save exists), revision-ordered imports under clock skew with divergent branches preserved in every direction and reviewable through the import choice, serialized single-tab saves without false conflicts, legacy `template-wizard-web` session migration from both the legacy database and legacy fallback keys with originals kept, deletion of migrated questionnaires that also removes the legacy copies so they cannot return, deletion that invalidates a queued save from a second tab, tied divergent session copies whose unselected real question answers stay reviewable across saves and reloads, recovery records from both stores and prior schemas, concurrent recovery writers that keep every acknowledged entry, a recovery write racing a deletion that leaves no preserved answers, eight simultaneous fallback-only tabs whose acknowledged preserved answers all survive a late stale writer, and deletion that reports failure when the primary cleanup fails even after a marker retry. Runtime requests must be GETs for same-origin static files. CI runs these checks before changes can pass validation.

## Release packaging

Run:

```bash
npm ci
make package
```

Packaging refuses a workspace with uncommitted or untracked changes and packages an exact committed snapshot: the staged tree ships only committed files (untracked and ignored workspace files are excluded from every archive), its files are verified byte for byte and — where the filesystem can represent Unix modes, as probed on the staged tree and the packaging origin — executable class against the packaged revision before anything is built, and the Runtime and Documentation bundles are built from the same staged snapshot, reporting byte-only verification honestly when the filesystem cannot carry modes. The privacy scan decodes every stored Git object — reachable or not, including binary payloads, commit headers, tag bodies, and tree entries — plus the plain Git support files; every classification is validated against a fictional positive control in both scan engines before scanning, and matches are reported only as redacted classifications, object identities, and locations. After archiving, the actual Source ZIP is extracted with the shipped file modes restored and re-verified: Git object integrity, project inventory exactly matching the committed tree, byte and mode agreement, the packaged commit, credential-free Git configuration, and a repeated redacted privacy scan. The runtime archive must match the built `dist/` byte for byte and the documentation archive must match the committed documentation files. The release workflow additionally refuses a tag that does not exactly match `v${VERSION}` or that does not resolve to the packaged commit.

`bash scripts/test-release-guards.sh` runs the required integrated canary battery: disposable clones carry fictional canaries for tracked content, commit messages, tag messages, binary blobs, dangling stored blobs, author headers, hidden index byte tricks, authenticated HTTP URL content, owner-execute mode changes, uniform mode values, and ignored bundle inputs; every negative case must reject packaging with the specific guard's evidence (path or build failures count as test failures, and a negative control asserts this), and the ignored-inputs case must package successfully without shipping fixtures in any bundle.

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
