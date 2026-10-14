# Security

## Security model

Runyth Born 2.x is a static client-side application. It has no account system, session API, database, application server, Docker socket, writable production volume, or backend secret.

The main sensitive data is questionnaire content entered by players.

## Local questionnaire state

Autosave data stays in the browser:

- IndexedDB is used first;
- `localStorage` is a compatibility fallback;
- no answer is intentionally transmitted to the static host by application code.

Saves are acknowledged only after the storage transaction commits, and every saved copy carries a monotonic revision. Authority is established across both storage copies: a copy with a lower revision than the newest acknowledged save anywhere is rejected instead of silently overwriting newer answers, so a stale second tab cannot destroy newer work merely by navigating, a recovered IndexedDB store cannot bury a newer acknowledged fallback save, and clock differences cannot make an older copy win.

Preserved copies (answers that no longer match a template, divergent portable imports in any revision direction, rejected conflicting edits, and divergent storage copies) are stored separately under a recovery record and stay available for review; the review dialog can copy, discard, or — for imported copies — apply the preserved answers. Both storage copies are consulted and merged: prior recovery schemas are upgraded, identical answers collapse, and copies acknowledged at different times or under a rolled-back clock stay side by side instead of one being discarded silently. Recovery writes read, merge, and commit inside one IndexedDB transaction, so concurrent tabs serially merge and an acknowledged preservation is never replaced by another writer's copy. Fallback rows are keyed by an identity generated for the live document, not by a value stored in sessionStorage, so a document that inherited another document's session storage cannot replace that document's acknowledged preservation. Readers union every existing row, including rows left by a reload.

The finish dialog offers a confirmed **Delete local copy** action that removes the session, its navigation position, the recovery record, and every copy left by the legacy migration from this browser, so a deleted questionnaire does not return on the next open. Recovery writes are gated by the deletion marker inside their commit transaction and their fallback writes, so a pending pre-deletion preservation cannot repopulate a removed questionnaire. Deletion first writes a marker that holds only the identifier and the deletion time into both storage copies; every save and migration re-checks that marker. A completed deletion is acknowledged only when a durable marker exists AND the record cleanup itself succeeded — a marker recorded on retry after a failed primary cleanup still reports incomplete removal, because durable invalidation alone does not erase a surviving readable copy. The fallback marker is retried after the removal frees space, and an unrecorded deletion reports incomplete removal instead of success. There is no automatic retention limit or central backup; on shared browser profiles, users who need to remove questionnaire data should use the delete control or the browser's site-data clearing.

Browsers may terminate a page before a debounced autosave completes. The most recent acknowledged **Saved locally** status, portable snapshots, and downloaded files are the recovery checkpoints, not every keystroke.

Browser extensions, compromised devices, browser profile access, or shared OS accounts can still expose locally stored data.

## Browser-origin trust and hosting

IndexedDB, `localStorage`, and browser storage are isolated per origin, not per path. GitHub Pages project sites under one hostname share a browser origin, so another project on the same origin can read these answer stores if its code is untrusted or compromised, and a Content Security Policy of `'self'` does not separate sibling projects. Two copies of this application on one origin also share their session stores.

When questionnaire confidentiality must be isolated from sibling sites, deploy to a dedicated origin. All code served from the chosen origin must be trusted; a deployment namespace or subpath reduces accidental collisions but creates no security boundary.

## Portable resume links

A portable resume link contains the questionnaire snapshot in the URL fragment (`#...`). Browsers do not include URL fragments in normal HTTP requests, so GitHub Pages does not receive that snapshot as part of the page request.

The snapshot is compressed/encoded, **not encrypted**. Anyone who receives the complete link can import and inspect the answers. Clipboard managers, browser history, screenshots, chat systems, and browser extensions may also retain the link.

Treat portable resume links as private data. Do not put a real link in public issues, logs, screenshots, or documentation.

Large questionnaires may create long URLs. URL-length limits belong to the browser and any software used to copy or transmit the link; they are not controlled by Runyth Born.

## Static-host exposure

Everything in `dist/` is public static content. Never place secrets, private templates, credentials, or private questionnaire data in `site/`, `templates/`, or other files copied into `dist/`.

GitHub Pages is a static publishing service, not an authentication boundary. If the entire site must be access-controlled, deploy the same `dist/` artifact behind a static host or edge access layer that provides authentication.

## Browser policy

The HTML pages include a restrictive Content Security Policy meta policy, `no-referrer`, and no third-party runtime assets. A static HTML meta policy cannot express every response-header security control; hosts that support custom security headers may additionally enforce them at the HTTP layer, including `frame-ancestors` if embedding is undesirable.

The wizard policy requires only same-origin scripts; it does not permit evaluation or WebAssembly compilation.

## Supply chain

Runtime application assets are repository JavaScript and generated JSON. Go is used only as a build-time template compiler. Playwright is a development-only browser-test dependency and is excluded from deployment and release runtime bundles. There is no CDN or third-party runtime code.

GitHub Actions workflows use least-purpose permissions. Checkout is configured with `persist-credentials: false`; release packaging therefore does not capture an Actions token inside the requested `.git` source archive.

## Release packaging

Release artifacts are built from an exact committed snapshot. The staging tree is verified byte for byte against the packaged commit before anything ships, and its executable classes are verified with the owner execute bit wherever the filesystem can represent modes (detected by chmod round-trip probes on the staged tree and the packaging origin); on a filesystem that cannot carry modes, verification reports the byte-only scope rather than claiming mode coverage. Hidden index state cannot substitute altered content, and the Runtime and Documentation bundles are built from the same staged snapshot so untracked or ignored inputs never leak into any archive. Runtime entries are compared byte for byte with the built output and Documentation entries with the committed files.

Every stored Git object of the packaged repository — reachable or not, including binary payloads, commit headers, tag bodies, and tree entries — is decoded and pattern-scanned, and the shipped plain Git files keep their scan. The pattern set is validated against a fictional positive control for every classification in both scan engines before scanning starts, and diagnostics are redacted to classification, object identity, and location, so detected values never reach the log. The actual published Source ZIP is extracted with shipped file modes restored, and its objects, inventory, revision, byte/mode match with the commit, and Git configuration are verified again.

A required integrated canary battery (`scripts/test-release-guards.sh`) plants fictional tokens in disposable clones for tracked content, commit messages, tag messages, binary blobs, dangling stored blobs, author headers, hidden index tricks, authenticated HTTP URL content, owner-execute mode changes, uniform mode values, and ignored bundle inputs, and requires the packaging run to refuse each case with evidence of the specific guard plus a negative control that path failures count as test failures.

## Input handling

Templates are parsed by the mdast JSON/YAML engine during the build. The compiler emits Markdown layout and answer slots as JSON; the browser inserts escaped literal answers into those slots. UI content is assigned through textContent and is never interpreted as HTML.

Portable imports cap both encoded input size and decompressed bytes before parsing. Imported state is reconciled with a published template before its fragment is removed from the URL.

The browser textarea retains the existing 65,536-character per-answer limit. The application does not evaluate template or answer text as JavaScript.

## Reporting

Do not disclose real questionnaire answers, portable resume URLs, repository secrets, or CI credentials in a public report.

Report security issues privately through GitHub private vulnerability reporting on this repository when it is available. If the reporting feature is unavailable, contact the maintainer privately by email: Daniel Braniewski [akki@ik.me](mailto:akki@ik.me).
