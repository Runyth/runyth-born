# Static-site audit

Audit date: 2026-10-04. Source of truth: local version 2.0 checkout.
Deployment preparation continued on 2026-10-05.

## Findings and fixes

| Finding | Resolution |
|---|---|
| The client loaded a Go WebAssembly program and runtime shim | Compile templates during the build; reconcile and download through plain JavaScript. Remove the WASM adapter and compilation permission from CSP. |
| IndexedDB reported successful saves before the transaction committed | Resolve on transaction completion; retain fallback when a successful request is followed by an abort. |
| In-flight saves read mutable state; navigation could clear a newer edit's dirty flag | Save snapshots and briefly disable answer entry while navigation saves and changes questions. Handle background save rejections. |
| Section controls relied on mouse events and ignored normal touch clicks | Use standard click activation; verify on a touch-enabled mobile viewport. |
| Cleared answers were replaced with template defaults on reconciliation | Apply explicitly saved empty answers. |
| Ordinal fallback IDs made duplicate labels appear unique | Count uniqueness by the label without its ordinal; ambiguous labels cannot transfer answers between moved sections. |
| Markdown punctuation in literal text could change formatting | Escape answer punctuation in JavaScript and template text in the build renderer; preserve multiline list indentation. Preserve space-only inline code. |
| Saved question position could shift after a template insertion | Relocate by stable question ID before falling back to a bounded integer index. |
| Export controls remained active when initialization failed | Disable actions until a valid session/template loads. Editing completed sessions resets their completion status. |
| Portable imports discarded their fragment before successfully loading the template | Reconcile and save first; then replace the fragment with the local session URL. |
| Compressed input was bounded only before decompression | Enforce an 18 MiB decoded limit while reading the stream; normalize imported position/completion values. |
| JSON parsing accepted trailing data | Require exactly one JSON document. |
| A crafted YAML sequence key could cause a panic | Remove the panic; reject merge keys in every mapping position and malformed/trailing inline JSON. |
| Question-like examples in blockquotes/task lists could become questions | Skip unsupported contexts during question extraction. |
| Unicode filenames could produce corrupted display titles | Capitalize the first Unicode rune rather than the first UTF-8 byte. |
| Uppercase template extensions and URL punctuation were mishandled by publication | Copy extensions case-insensitively and encode compiled-template URL paths. Reject generated/reserved filenames. |
| Relative 404 links broke on nested missing paths | Generate a deployment base from Pages configuration; preserve relative normal-page URLs. |
| Output overrides could delete source directories | Reject source directories and project ancestors before cleanup. |
| Artifact validation covered only HTML/WASM, not the browser's full dependencies | Validate module imports, compiled template fingerprints/slots, symlinks, and absence of Go/WASM/executable/server files. |
| Future CI changes could break browser-only behavior without detection | Add pinned development-only Chromium tests to CI, Pages, and release workflows; exclude test dependencies from published runtime/source bundles. |
| The browser harness rewrote 404 base URLs and could hide a publication error | Build root and project artifacts independently, serve every file unchanged, and verify the exact 404 response and recovery URL. |

## Three verification layers

1. Source and workflow checks: Go formatting, race tests, vet, JavaScript syntax,
   regression tests, and actionlint/YAML parsing.
2. Build and artifact checks: generated catalog parity, compiled slots and
   fingerprints, all local HTML/module references, pure static files, and all
   three release ZIP contents.
3. Actual browser checks: root and `/template-wizard/` project URLs, mobile touch navigation,
   autosave/reload, clearing, escaped multiline Markdown downloads, completion,
   fresh-context portable resume, localStorage fallback, missing-session errors,
   and deep 404 recovery. Runtime traffic is restricted to same-origin GETs.

Outcome: all three layers passed. The Go tests (with race detection), 12 JavaScript regression tests, artifact smoke checks, actionlint, and Chromium acceptance completed successfully. Additional isolated builds verified uppercase JSON/YAML extensions and Unicode/punctuation filenames; output-directory guards rejected source paths. All three release ZIPs were inspected.

Reproduce the main checks:

```bash
make check
npm ci
npx playwright install chromium
make browser-check
make package
actionlint .github/workflows/*.yml
```

For project-hosted 404 recovery, build with
`SITE_BASE_PATH=/project-name make build`; Pages obtains this automatically.

## Scope and limits

The connected GitHub issue search found no issues for `Akkitto/template-wizard`;
repository metadata also reports that its issue tracker is disabled. Its remote
`master` branch still contains the older terminal application. The local
`upstream` remote now targets that repository.

Publication attempts through the connected GitHub integration returned HTTP 403
(`Resource not accessible by integration`) for both Git tree and blob creation.
The user enabled Pages with GitHub Actions, and repository metadata confirms
`has_pages: true`. Local Git's initial push dry run could not obtain credentials;
the user subsequently completed GitHub CLI browser authentication as `Akkitto`.
The authenticated Pages API confirms `build_type: workflow` and enforced HTTPS.
Publication retains the upstream project's existing AGPL-3.0 license.

The three verification layers above validate the local artifact and workflow.
Use the `LIVE_SITE_URL` command in `OPERATIONS.md` to run the same browser
acceptance checks against the published URL after its Actions deployment
succeeds. The checks are not evidence that the application has no possible bugs
or security issues. Chromium was exercised; other browser engines were not
tested in this audit.

The site requires no production Go application or service. Go remains a
build-time template compiler, and Playwright is only a test dependency.
