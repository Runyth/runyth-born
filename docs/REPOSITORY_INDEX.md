# Repository index

This index describes the local version 2.0 checkout (`3e3e332` before this audit),
not the older terminal application found through the connected GitHub account.

## Build and deployment

`make build` runs `scripts/build-static.sh`, which copies `site/`, publishes
source templates, and runs `cmd/template-catalog` to produce a catalog and
compiled JSON for each template. The build validates the finished artifact.

Go is only a build-time tool. The deployed `dist/` contains HTML, CSS, JavaScript,
JSON, and text files. It contains no Go executable, WebAssembly, runtime shim,
API, database, or server configuration.

`.github/workflows/pages.yml` validates, builds, browser-tests, uploads `dist/`,
and deploys through the official Pages actions. The repository's Pages source
must be set to GitHub Actions. Its configured base path is used for deep 404
recovery; ordinary application URLs and module imports are relative.

## Source map

| Location | Responsibility |
|---|---|
| `site/index.html`, `site/assets/home.js` | Catalog cards, new sessions, recent sessions |
| `site/wizard.html`, `site/assets/wizard.js` | Questions, section navigation, progress, autosave scheduling, completion, downloads |
| `site/assets/runtime.js` | Plain JavaScript reconciliation, section counts, escaped answer insertion into compiled Markdown slots |
| `site/assets/storage.js` | Session creation, cryptographic IDs, IndexedDB commit confirmation, localStorage fallback |
| `site/assets/portable.js` | Bounded gzip/JSON resume snapshots, URL-fragment encoding/import |
| `site/assets/app.css` | Responsive layout and UI styling |
| `site/404.html` | Deployment-aware recovery from missing URLs |
| `templates/`, `examples/` | Published inputs and authoring examples |
| `cmd/template-catalog/main.go` | Build-time catalog and compiled-template generator |
| `internal/staticapp/compile.go` | Precomputed Markdown layout and multiline answer slots |
| `internal/staticapp/core.go` | Build-time template analysis and section metadata |
| `internal/mdast/` | JSON/YAML loading, question discovery, IDs, merging, fingerprints |
| `internal/markdown/` | Canonical mdast Markdown rendering |
| `internal/yamlmini/` | Dependency-free JSON-compatible YAML subset |
| `scripts/check.sh` | Formatting, race tests, vet, JS tests/syntax, build and artifact smoke checks |
| `scripts/validate-static.py` | Artifact references, compiled slots, catalog parity, no server/Go/WASM assets |
| `scripts/package-release.sh` | Source/history, deployable runtime, and documentation ZIPs |
| `tests/js/` | Storage, resume, reconciliation regressions and artifact smoke tests |
| `tests/browser/smoke.mjs` | Chromium acceptance of unchanged root/project artifacts, or the actual deployment via `LIVE_SITE_URL` |
| `package.json`, `package-lock.json` | Pinned development-only browser testing dependency |

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
