# Security

## Security model

Template Wizard Web 2.x is a static client-side application. It has no account system, session API, database, application server, Docker socket, writable production volume, or backend secret.

The main sensitive data is questionnaire content entered by players.

## Local questionnaire state

Autosave data stays in the browser:

- IndexedDB is used first;
- `localStorage` is a compatibility fallback;
- no answer is intentionally transmitted to the static host by application code.

Browser extensions, compromised devices, browser profile access, or shared OS accounts can still expose locally stored data.

## Portable resume links

A portable resume link contains the questionnaire snapshot in the URL fragment (`#...`). Browsers do not include URL fragments in normal HTTP requests, so GitHub Pages does not receive that snapshot as part of the page request.

The snapshot is compressed/encoded, **not encrypted**. Anyone who receives the complete link can import and inspect the answers. Clipboard managers, browser history, screenshots, chat systems, and browser extensions may also retain the link.

Treat portable resume links as private data. Do not put a real link in public issues, logs, screenshots, or documentation.

Large questionnaires may create long URLs. URL-length limits belong to the browser and any software used to copy or transmit the link; they are not controlled by Template Wizard.

## Static-host exposure

Everything in `dist/` is public static content. Never place secrets, private templates, credentials, or private questionnaire data in `site/`, `templates/`, or other files copied into `dist/`.

GitHub Pages is a static publishing service, not an authentication boundary. If the entire site must be access-controlled, deploy the same `dist/` artifact behind a static host or edge access layer that provides authentication.

## Browser policy

The HTML pages include a restrictive Content Security Policy meta policy, `no-referrer`, and no third-party runtime assets. A static HTML meta policy cannot express every response-header security control; hosts that support custom security headers may additionally enforce them at the HTTP layer.

The wizard policy requires only same-origin scripts; it does not permit evaluation or WebAssembly compilation.

## Supply chain

Runtime application assets are repository JavaScript and generated JSON. Go is used only as a build-time template compiler. Playwright is a development-only browser-test dependency and is excluded from deployment and release runtime bundles. There is no CDN or third-party runtime code.

GitHub Actions workflows use least-purpose permissions. Checkout is configured with `persist-credentials: false`; release packaging therefore does not capture an Actions token inside the requested `.git` source archive.

## Input handling

Templates are parsed by the mdast JSON/YAML engine during the build. The compiler emits Markdown layout and answer slots as JSON; the browser inserts escaped literal answers into those slots. UI content is assigned through textContent and is never interpreted as HTML.

Portable imports cap both encoded input size and decompressed bytes before parsing. Imported state is reconciled with a published template before its fragment is removed from the URL.

The browser textarea retains the existing 65,536-character per-answer limit. The application does not evaluate template or answer text as JavaScript.

## Reporting

Do not disclose real questionnaire answers, portable resume URLs, repository secrets, or CI credentials in a public report. Report security issues privately to the repository owner.
