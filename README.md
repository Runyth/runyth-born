# Runyth Born

Runyth Born is a completely static browser application for filling reusable Markdown questionnaires correctly. Version 3.2.2 requires no application server, database, container, reverse proxy, writable server volume, or backend API.

The deployed site consists only of HTML, CSS, JavaScript, and repository-backed JSON template data, making GitHub Pages through GitHub Actions the preferred deployment target.

## What it does

- Reads raw mdast templates stored as JSON or YAML in the repository.
- Discovers headings, sections, context, and fillable `- **Question:**` entries generically.
- Guides the player through one question at a time with section and overall progress.
- Autosaves questionnaire state locally in the browser.
- Reconciles saved answers when a compatible template changes under the same filename.
- Renders the complete Markdown document in the browser and downloads it as a normal `.md` file.
- Creates portable resume links containing a compressed snapshot in the URL fragment, allowing another browser or device to resume without a backend.
- Loads no CDN, analytics, advertising, or third-party browser application code.

## Architecture

| Component | Purpose |
|---|---|
| `site/` | Static HTML, CSS, and JavaScript source |
| `templates/` | Version-controlled mdast JSON/YAML templates |
| Build-time template compiler | Parses mdast/YAML and prepares Markdown layout before deployment |
| JavaScript client | Reconciles answers and assembles Markdown entirely in the browser |
| IndexedDB | Primary local questionnaire autosave storage |
| `localStorage` | Compatibility fallback when IndexedDB is unavailable |
| URL fragment | Portable compressed resume snapshot; fragments are not sent in HTTP requests |
| GitHub Actions | Tests, builds the static artifact, and deploys it to GitHub Pages |

There is no runtime backend. The `dist/` build artifact is directly deployable to any ordinary static host.

## GitHub Pages deployment

The repository is designed around the `master` branch and `.github/workflows/pages.yml`.

1. Push the repository to GitHub.
2. Open **Settings → Pages**.
3. Under **Build and deployment**, select **GitHub Actions** as the source.
4. Push to `master` or run the **Deploy GitHub Pages** workflow manually.
5. GitHub Actions runs the quality gates, builds `dist/`, uploads the Pages artifact, and deploys it.

No Go program, WebAssembly runtime, Node.js process, or service runs in production. Go is only a build-time template compiler, like a static site generator.

No `gh-pages` branch is required. Relative asset paths make the site work both at an account root and below a project path such as `https://example.github.io/runyth-born/`.

## Local development

Requirements:

- Go 1.23 or newer, used only during the static build;
- Node.js 22 or newer for JavaScript validation/tests;
- Python 3 for the optional local static preview and static-artifact validator.

Run all quality gates:

```bash
make check
```

Build the deployable site:

```bash
make build
```

Preview it locally, before committing or deploying:

```bash
make serve
```

Then open `http://127.0.0.1:8080/`. The command builds `dist/`, serves only that directory on `127.0.0.1`, and rebuilds when `site/` or `templates/` changes. Refresh the browser to see the edit. Stop the server with Ctrl+C. A local HTTP server is required because browsers restrict ES modules and `fetch` on `file://` URLs. It is not part of the deployed architecture. Use `PORT=8765 make serve` for another port.

Browser acceptance tests use a development-only Playwright dependency:

```bash
npm ci
npx playwright install chromium
make browser-check
```

## Adding or updating templates

Place mdast files in `templates/`:

```text
templates/
├── character-backstory.mdast.json
├── downtime-questionnaire.mdast.json
└── faction-design.mdast.yaml
```

Supported extensions are `.json`, `.yaml`, and `.yml`. The build generates `templates/catalog.json` and `*.compiled.json` automatically; do not maintain these files by hand. The original mdast files are also published. Names `catalog.json` and `*.compiled.json` are reserved for generated files.

Unlike 1.x, a running site cannot dynamically scan a server directory. Adding, removing, or replacing a template therefore requires a new static build/deployment. GitHub Actions performs that rebuild automatically after a push to `master`.

When a template changes under the same filename, compatible saved answers are merged by:

1. heading breadcrumb;
2. question label;
3. occurrence number;
4. globally unique label fallback.

Keep section headings and question labels stable when the meaning of a question remains stable.

## Creating a template

Write ordinary Markdown using the questionnaire grammar in [TEMPLATE_FORMAT.md](TEMPLATE_FORMAT.md). A chapter is a `##` heading followed by either one question list or deeper headings, never both. Guidance stays with the heading it follows, and a deeper heading keeps it:

```markdown
# Character Backstory

## Core

Brief answers are sufficient.

- **Name:**
- **Age:**
- **Origin - What culture or community raised me?:**
```

Convert it with `markdown-rs-cli`:

```bash
markdown-rs-cli \
  --from markdown \
  --dialect gfm \
  character-backstory.md \
  --output character-backstory.mdast.json
```

Commit the generated mdast file to `templates/`. See [TEMPLATE_FORMAT.md](TEMPLATE_FORMAT.md) for the exact structural contract. The canonical authoring prompt is [AI_TEMPLATE_PROMPT.md](AI_TEMPLATE_PROMPT.md) and is published as `ai-template-prompt.txt` in the static site.

## Player workflow

1. Open the site.
2. Choose a template and optionally enter a player name.
3. Answer each prominently displayed question.
4. Navigate with Previous/Next, the section list, or `Alt` + Left/Right Arrow.
5. Autosave keeps the working questionnaire in the current browser.
6. Use **Copy resume link** when a portable cross-device snapshot is needed.
7. Download the current Markdown at any time, or use **Finish and download** on completion.
8. Send the Markdown file to the Dungeon Master.
9. Optionally use **Delete local copy** in the finish dialog to remove the questionnaire from this browser.

## Saving and resume links

The normal wizard URL contains only a random local session identifier and resumes from browser storage on that device.

**Copy resume link** creates a different, self-contained URL whose fragment contains the current questionnaire snapshot. The fragment:

- is compressed when the browser supports native compression streams;
- is not transmitted to GitHub Pages in HTTP requests;
- is not encrypted;
- contains the player name and answers needed to resume;
- can be imported by another browser serving the same template filename.

Treat the complete portable URL as private. Very large questionnaires can also produce very large URLs, and browsers or messaging systems may impose their own URL-length limits.

## 1.x migration

2.0.0 is intentionally breaking. Server-side 1.x session files and `/w/<token>` URLs are not part of the static architecture and cannot be resumed automatically by a 2.x deployment. Export any important 1.x Markdown/session data before replacing the old server deployment.

Templates themselves remain compatible and can be copied into the new repository `templates/` directory.

## Release artifacts

`make package` creates exactly three release ZIPs:

- `*-src.zip` - complete Git repository, including `.git` history;
- `*-run.zip` - built static `dist/` contents, ready for static hosting;
- `*-doc.zip` - documentation-only bundle.

## Licence
Copyright © 2026  [Daniel Braniewski](https://brani.dev/)

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.