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
- Node.js unit tests for session/resume encoding;
- a complete static build;
- plain JavaScript reconciliation/rendering against every published template;
- generated template catalog integrity;
- HTML local-asset references;
- absence of Go/Docker/server deployment files from the runtime artifact;
- absence of WebAssembly and `wasm_exec.js`;
- generated answer-slot integrity and local JavaScript module imports.

## Automated browser acceptance

```bash
npm ci
npx playwright install chromium
make browser-check
```

Chromium checks root and project paths, local reload/resume, touch navigation, cleared answers, literal Markdown downloads, completion, portable import in a fresh browser context, localStorage fallback, missing sessions, and nested 404 recovery. Runtime requests must be GETs for same-origin static files. CI runs these checks before changes can pass validation.

## Release packaging

Run:

```bash
make package
```

Expected artifacts:

```text
release/template-wizard-web-<version>-src.zip
release/template-wizard-web-<version>-run.zip
release/template-wizard-web-<version>-doc.zip
```

The source ZIP intentionally contains `.git` and its available repository history. The runtime ZIP contains only deployable static-site files. The documentation ZIP contains no runtime binaries.

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
