# Template Format

Runyth Born reads raw mdast documents from the repository `templates/` directory. Supported file extensions are `.json`, `.yaml`, and `.yml`.

The build validates templates and generates the static `dist/templates/catalog.json` and `*.compiled.json` files automatically. Only generated JSON and plain JavaScript are needed at runtime; Go is a build-time tool. A template change therefore requires a rebuild/redeployment; there is no runtime directory scan in 2.x.

## Supported YAML subset

The build accepts a dependency-free, JSON-compatible YAML subset. Supported:
mappings, sequences, plain and quoted scalars, inline JSON arrays/objects,
`|` and `>` block scalars with optional chomping indicators (`-`, `+`) and
explicit indentation indicators (for example `|2-`). Rejected: YAML anchors,
aliases, tags, merge keys, and tab indentation. Syntax outside the subset fails
the build instead of being interpreted loosely; accepted forms keep their
specified meaning, including extra indentation inside folded scalars, comments
after block scalar headers, quoted hashes inside inline JSON strings, and
comments around inline JSON collections.

## Required mdast root

The top-level value must be an mdast root object with a `children` array:

```json
{
  "type": "root",
  "children": []
}
```

## Fillable question convention

A fillable question must be the first paragraph inside a list item and begin with a `strong` node whose plain text ends in a colon. In ordinary Markdown, write:

```markdown
- **Name:**
- **Age:**
- **Origin - What culture or community raised me?:**
```

The answer is inserted after the bold label, never inside it.

Questions inside blockquotes, tables, code, HTML, footnote definitions, and task list items are excluded.

Existing answer text is recognised too:

```markdown
- **Name:** Akito
```

## Sections and context

Headings establish the current breadcrumb. Question sections are derived from that breadcrumb. Ordinary paragraphs and non-question list items before a question are carried into the guided UI as context, with a bounded recent-context window.

Example:

```markdown
# Character Backstory

## Core

Brief answers are sufficient.

- **Name:**
- **Age:**

## Origin

Describe what shaped the character before adventuring.

- **Where were you raised?:**
```

## Stable template evolution

Saved answers are reconciled with compatible newer template revisions using:

1. heading breadcrumb + question label + occurrence;
2. unique label-based fallback.

For safe evolution:

- keep the template filename stable;
- keep a heading stable when its semantic section is unchanged;
- keep a question label stable when its meaning is unchanged;
- add new questions rather than repurposing old labels;
- avoid duplicate labels unless the surrounding breadcrumb clearly distinguishes them.

Renaming or removing the template file can make existing local sessions and portable resume links unable to locate their template.

## Creating mdast from Markdown

`markdown-rs-cli` can convert ordinary Markdown into mdast JSON:

```bash
markdown-rs-cli \
  --from markdown \
  --dialect gfm \
  character-backstory.md \
  --output character-backstory.mdast.json
```

Commit the resulting file to `templates/`, then run `make check` or push to `master` so GitHub Actions validates and redeploys the static catalog.
