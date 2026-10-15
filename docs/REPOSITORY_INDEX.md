# Repository index

This index describes the current Runyth Born static browser application.

## Build and deployment

`make build` runs `scripts/build-static.sh`, which copies `site/`, publishes
source templates, publishes the licence text as `license.txt`, and runs
`cmd/template-catalog` to produce a catalog and compiled JSON for each
template. The build refuses output directories that overlap tracked source and
validates the finished artifact.

Go is only a build-time tool, pinned by the `toolchain` directive in `go.mod`.
The deployed `dist/` contains HTML, CSS, JavaScript, JSON, and text files. It
contains no Go executable, WebAssembly, runtime shim, API, database, or server
configuration.

`.github/workflows/pages.yml` validates, builds, browser-tests, uploads `dist/`,
and deploys through official Pages actions pinned to commit SHAs. The
repository's Pages source must be set to GitHub Actions. Its configured base
path is used for deep 404 recovery; ordinary application URLs and module
imports are relative.

## Source map

| Location | Responsibility |
|---|---|
| `site/index.html`, `site/assets/home.js` | Catalog cards, new sessions, recent sessions |
| `site/wizard.html`, `site/assets/wizard.js` | Questions, section navigation, progress, autosave scheduling, conflict handling, preserved-answer review, completion, downloads, delete control |
| `site/assets/runtime.js` | Plain JavaScript reconciliation with match diagnostics, section counts, literal-text escaping for answers and preserved copies |
| `site/assets/storage.js` | Session creation, cryptographic IDs, cross-copy revision authority, atomic recovery reads/merges/commits, per-document fallback recovery rows that do not trust cloneable sessionStorage, deletion markers preserving final removal under quota pressure with failed primary cleanup reported on every marker branch, navigation position store, recovery records with schema upgrades and cross-store merges, legacy-namespace migration, localStorage fallback, scoped deletion |
| `site/assets/portable.js` | Bounded gzip/JSON resume snapshots with save revisions, URL-fragment encoding/import |
| `site/assets/app.css` | Responsive layout and UI styling |
| `site/404.html` | Deployment-aware recovery from missing URLs |
| `templates/`, `examples/` | Published inputs and authoring examples |
| `cmd/template-catalog/main.go` | Build-time catalog and compiled-template generator |
| `internal/staticapp/compile.go` | Precomputed Markdown layout and multiline answer slots |
| `internal/staticapp/core.go` | Build-time template analysis and section metadata |
| `internal/mdast/` | JSON/YAML loading, question discovery, IDs, merging, fingerprints |
| `internal/markdown/` | Canonical mdast Markdown rendering with literal-text escaping |
| `internal/yamlmini/` | Dependency-free JSON-compatible YAML subset with full block scalar semantics, reference-compatible plain-scalar folding, quoted mapping keys, flow collections, and nested block indents |
| `scripts/check.sh` | Formatting, race tests, vet, JS tests/syntax, build and artifact smoke checks |
| `scripts/build-static.sh` | Static build with tracked-source output protection |
| `scripts/preview-static.py` | Local `127.0.0.1` preview of `dist/` that rebuilds when site or template sources change |
| `scripts/validate-static.py` | Artifact references, compiled slots, catalog parity, no server/Go/WASM assets |
| `scripts/package-release.sh` | Provenance checks, history scan, three release ZIPs, byte/mode verification, archive verification with extracted-source checks, checksums, physical isolation of nested Git-directory symlinks and copied worktree redirects |
| `scripts/check-release-tag.sh` | Release tag/VERSION agreement guard |
| `scripts/scan-source-history.sh` | Redacted credential scan over every stored Git object, decoded with headers and binary payloads, per-engine validated patterns, plus Git support files |
| `scripts/verify-committed-tree.sh` | Byte and executable-class verification of a tree against the packaged revision with filesystem probes |
| `scripts/test-release-guards.sh` | Canary battery running the real packaging in disposable clones with exact guard-evidence assertions |
| `tests/js/` | Storage, resume, reconciliation, build-guard, release-tag, and Markdown fidelity regressions plus artifact smoke tests |
| `tests/browser/smoke.mjs` | Chromium acceptance of unchanged root/project artifacts, or the actual deployment via `LIVE_SITE_URL` |
| `tests/browser/writer-isolation.mjs` | Native Chromium probe of inherited sessionStorage tokens under a mixed COOP response policy |
| `package.json`, `package-lock.json` | Pinned development-only browser testing and Markdown validation dependencies |

## Data flow

Build: mdast JSON/YAML -> validated document -> stable question IDs + layout
slots -> static catalog and compiled JSON.

Browser: catalog -> create/load local session -> fetch compiled JSON -> match
saved answers -> navigate/edit -> save locally -> insert escaped answers into
layout -> download Markdown or encode a portable fragment.

Saved question IDs use heading breadcrumbs, label, and occurrence. Fallback
matches apply only when a label is globally unique in both revisions. Cleared
answers override defaults. Template filenames must remain stable for resume.

## Runtime boundaries

All application requests are GETs for same-origin static assets. Answers are
stored in the browser or included in a user-requested portable link fragment.
The fragment is encoded/compressed, not encrypted. There is no central backup.
Adding templates requires rebuilding. Modern JavaScript/browser storage is
required; local development uses an HTTP static-file preview rather than file://.

See `docs/STATIC_AUDIT.md` for findings and validation evidence.
