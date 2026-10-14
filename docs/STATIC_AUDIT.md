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
| Markdown punctuation in literal text could change formatting | Escape answer punctuation in JavaScript and template text in the build renderer; preserve multiline list indentation. Preserve space-only inline code. Ampersands are escaped so HTML-entity strings stay literal. |
| Saved question position could shift after a template insertion | Relocate by stable question ID before falling back to a bounded integer index. Navigation positions are stored separately from answers. |
| Export controls remained active when initialization failed | Disable actions until a valid session/template loads. Editing completed sessions resets their completion status. |
| Portable imports discarded their fragment before successfully loading the template | Reconcile and save first; then replace the fragment with the local session URL. |
| Compressed input was bounded only before decompression | Enforce an 18 MiB decoded limit while reading the stream; normalize imported position/completion values. |
| JSON parsing accepted trailing data | Require exactly one JSON document. |
| A crafted YAML sequence key could cause a panic | Remove the panic; reject merge keys in every mapping position and malformed/trailing inline JSON. |
| Question-like examples in blockquotes/task lists could become questions | Skip unsupported contexts during question extraction. |
| Unicode filenames could produce corrupted display titles | Capitalize the first Unicode rune rather than the first UTF-8 byte. |
| Uppercase template extensions and URL punctuation were mishandled by publication | Copy extensions case-insensitively and encode compiled-template URL paths. Reject generated/reserved filenames. |
| Relative 404 links broke on nested missing paths | Generate a deployment base from Pages configuration; preserve relative normal-page URLs. |
| Output overrides could delete source directories | Reject tracked-source overlaps by validating the resolved destination against the tracked inventory, project ancestors, and the Git directory; without Git, only the default `dist` directory is accepted in-repository. |
| Packaging through a checkout symlink could clean the original source | Physically canonicalize both packaging roots and verify the staging copy is a real isolated directory under the temporary root before any destructive cleanup. |
| Artifact validation covered only HTML/WASM, not the browser's full dependencies | Validate module imports, compiled template fingerprints/slots, symlinks, and absence of Go/WASM/executable/server files. |
| Future CI changes could break browser-only behavior without detection | Add pinned development-only Chromium tests to CI, Pages, and release workflows; exclude test dependencies from published runtime/source bundles. |
| The browser harness rewrote 404 base URLs and could hide a publication error | Build root and project artifacts independently, serve every file unchanged, and verify the exact 404 response and recovery URL. |
| Stale tabs, template updates, and clock skew could silently replace or discard saved answers | Guard saves with monotonic revisions in a read-compare-update transaction, store navigation positions separately, order divergent copies by revision, and preserve unmatched or displaced answers in a reviewable recovery record. |
| The rename to Runyth Born changed browser storage names | Migrate legacy `template-wizard-web` database and fallback sessions into the renamed stores on first open, keeping the legacy originals. |
| Save authority checked only its destination store | Establish revision authority across both storage copies before writing; tie divergent pairs explicitly by loading the selected record and preserving the other. |
| Fallback-acknowledged recovery records became invisible when the primary store recovered | Read recovery records from both copies and choose the newest acknowledged record. |
| Invoking the build through a symlinked checkout bypassed the source guard | Canonicalize both project roots physically before all tracked-source comparisons. |
| Fallback-only legacy sessions did not migrate, and divergent legacy copies selected the older answer | Migrate legacy fallback keys without IndexedDB and select the fresher legacy copy by the legacy wall-clock rule, keeping the unselected original. |
| Deleting a migrated questionnaire returned through re-migration | Remove the legacy database and fallback copies with the session and report incomplete removal only when an IndexedDB copy may survive. |
| Preserved-answer serialization emitted raw text and headings | Route protected answers and headings through the literal text serializer. |
| Portable branches with lower local revisions were silently dropped | Preserve every divergent branch regardless of revision counter and offer review or import for imported copies. |
| One tab falsely conflicted with its own serialized saves | Carry the acknowledged revision into queued snapshots at execution time. |
| The renderer regression silently skipped in dependency-free gates | Install dependencies before the gates and fail loudly when the validator is unavailable. |
| Tied divergent session copies tested only a synthetic top-level answer field | Extract the unselected record's real `questions` entries, await the preservation, and test with plain persisted-session fixtures. |
| Prior recovery schemas and clock-rolled-back copies were ignored or replaced | Normalize both recovery shapes, merge entries across both stores, and let writes carry the union so upgrades never lose answers. |
| A save queued before deletion could recreate the removed session | Publish deletion markers holding only the ID and time in both stores, re-checked by every save, migration skip, transaction, and fallback write. |
| Block scalars still differed from reference parsers on explicit depth, EOF, spaces, and indicators | Replay the reference scanner semantics: physical line-break records, scan-depth fixed by the indicator, significant-space blanks, ASCII 1–9 indicators, and rejected structural tabs. |
| The scanner skipped binary objects, write-only blobs, and commit headers | Decode every stored object reachable from the object database with redacted object-level diagnostics. |
| Ignored inputs leaked into Runtime and Documentation archives | Build every bundle from the staged committed snapshot instead of the working tree. |
| Byte/mode verification could be bypassed through hidden index state | Compare each tracked file's disk bytes and mode with the packaged commit directly. |
| ZIP extraction dropped the shipped file modes | Restore the archived modes during verification. |
| Concurrent recovery writers could replace each other's acknowledged entries | Read, merge, and commit recovery records inside one transaction; the fallback mirror re-checks after writing and merges an interfering writer. |
| Recovery writes and fallback quota pressure could restore deleted questionnaires | Gate recovery writes inside the deletion-consulting commit transaction, retry the fallback marker after freeing space, and report completed removal only with a durable marker. |
| POSIX whitespace classes were accepted by the Python object scanner without their POSIX meaning | Keep per-engine pattern forms with a runtime placeholder and validate every classification against a positive control in both engines. |
| The mode verifier compared group execute and silenced itself on uniform trees | Verify the owner execute class, gate the reduced scope on chmod capability probes only, and report byte-only verification honestly. |
| The canary battery failed on clean clones and accepted unrelated failures as rejections | Gate the overlay commit on real changes, keep setup failures fatal, require exact per-guard evidence, and run a negative-control case. |
| Quoted hashes inside supported inline JSON read as YAML comments | Locate the JSON boundary with string/escape awareness and validate comments only around the collection. |
| Quoted mapping keys were parsed as standalone scalars | Distinguish a quoted key from a quoted value before scalar dispatch, including nested blocks. |
| Malformed NBSP-plus-tab input panicked the build parser | Return a validation error for a structural line that is empty after trimming, before indexing it. |

## Three verification layers

1. Source and workflow checks: Go formatting, race tests, vet, JavaScript syntax,
   regression tests, and actionlint/YAML parsing.
2. Build and artifact checks: generated catalog parity, compiled slots and
   fingerprints, all local HTML/module references, pure static files, and all
   three release ZIP contents.
3. Actual browser checks: root and `/runyth-born/` project URLs, mobile touch navigation,
   autosave/reload, clearing, escaped multiline Markdown downloads, completion,
   fresh-context portable resume, localStorage fallback, missing-session errors,
   and deep 404 recovery. Runtime traffic is restricted to same-origin GETs.

Outcome: all three layers passed. The Go tests (with race detection), JavaScript regression tests, artifact smoke checks, actionlint, and Chromium acceptance completed successfully. Additional isolated builds verified uppercase JSON/YAML extensions and Unicode/punctuation filenames; output-directory guards rejected tracked-source overlaps in disposable copies. All three release ZIPs were inspected.

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
`SITE_BASE_PATH=/runyth-born make build`; Pages obtains this automatically.

## Scope and limits

The audit is a source, history, artifact, and selected-behavior review, not a
penetration test or a formal proof of safety. Chromium was exercised; other
browser engines were not tested in this audit. Short bounded fuzzing cannot
establish arbitrary-input robustness. Template authors and same-origin
application code remain trusted, and browser-origin sibling projects share
storage (see `SECURITY.md`).

The site requires no production Go application or service. Go remains a
build-time template compiler, and Playwright is only a test dependency.
