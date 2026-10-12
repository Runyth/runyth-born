# Operations

Runyth Born 2.x has no production application process. Operations consist of building and publishing immutable static files.

## Preferred deployment: GitHub Pages

The authoritative deployment workflow is `.github/workflows/pages.yml`.

Required repository setting:

1. Open **Settings → Pages**.
2. Set **Build and deployment → Source** to **GitHub Actions**.

A push to `master` then performs:

1. source checkout;
2. Go and JavaScript quality gates;
3. template catalog generation;
4. build-time template compilation to plain JSON;
5. static artifact validation;
6. Pages artifact upload;
7. GitHub Pages deployment.

No Docker host, reverse proxy, TLS configuration, persistent volume, database, backup job, or process supervisor is required.

After the Pages workflow succeeds, verify the actual deployment with the same
browser acceptance suite (requires the development test dependencies):

```bash
SITE_BASE_PATH=/runyth-born make build
LIVE_SITE_URL=https://runyth.github.io/runyth-born/ npm run test:browser
```

This exercises the live homepage, autosave/reload, touch navigation, Markdown
download, portable resume, localStorage fallback, and GitHub's nested 404 page.
The built local 404 response must match the published response byte for byte.

## Generic static hosting

Build locally or in CI:

```bash
make build
```

Publish the contents of `dist/` as the document root. Only ordinary HTML, CSS, JavaScript, JSON, and text files are served. No Go program or WebAssembly runtime is deployed.

For a deployment below a project path, build with `SITE_BASE_PATH=/project-name make build`. This configures the custom 404 page to return to the correct homepage even from a deeply nested missing URL. The Pages workflow obtains this path from `actions/configure-pages`; normal pages use relative URLs.

Do not deploy the repository root as the document root. Deploy `dist/` or the `*-run.zip` contents.

## Template changes

Templates are immutable deployment inputs. Commit a changed file under `templates/` and redeploy. `templates/catalog.json` is generated during the build and must not be edited manually.

Compatibility across template revisions depends on stable heading breadcrumbs and question labels. Removing or renaming the template file itself can prevent saved sessions and portable links from reopening it.

## Persistence

Questionnaire state is client-side only:

- IndexedDB is the primary autosave store;
- `localStorage` is the fallback;
- portable resume links embed a compressed snapshot in the URL fragment.

There is no central session backup because there is no server-side session store. Players who need cross-device or durable transfer should copy the portable resume link and keep the downloaded Markdown file.

## Rollback

GitHub Pages deployments are immutable build artifacts. To roll back, redeploy a known-good Git commit/tag. A rollback also restores that revision's template files; local browser sessions remain client-side and will reconcile against the deployed compatible template when possible.

## Release

Create the three release bundles:

```bash
make package
```

For tagged GitHub releases, `.github/workflows/release.yml` runs the same validation and publishes the generated ZIPs.
