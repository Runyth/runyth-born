# Template Format

Runyth Born reads raw mdast documents from the repository `templates/` directory. Supported file extensions are `.json`, `.yaml`, and `.yml`.

The build validates templates and generates the static `dist/templates/catalog.json` and `*.compiled.json` files automatically. Only generated JSON and plain JavaScript are needed at runtime; Go is a build-time tool. A template change therefore requires a rebuild/redeployment; there is no runtime directory scan in 2.x.

## Supported YAML subset

The build accepts a dependency-free, JSON-compatible YAML subset. Supported:
mappings (including quoted keys), sequences, plain and quoted scalars, inline JSON arrays/objects,
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

## Questionnaire schema

The build rejects a template that does not match this grammar. There is no alternate interpretation.

```text
document      = H1, preamble?, chapter+
preamble      = paragraph+
chapter       = H2, guidance?, (question-list | subchapter+)
subchapter    = H3, guidance?, (question-list | detail+)
detail        = H4, guidance?, question-list
guidance      = (paragraph | example-list)+
question-list = one unordered list; every item is exactly "- **Label:**"
example-list  = one unordered list; no item is a question
```

Rules:

- The first node is one depth-1 title. Depths 5 and 6 are rejected. A heading may not skip a depth.
- The preamble is zero or more paragraphs after the title and before the first chapter. It is shown with the first question and is not repeated later.
- A chapter is one depth-2 heading. It then contains either one question list or one or more subchapters, never both, and never neither.
- A subchapter is one depth-3 heading. It then contains either one question list or one or more details, never both.
- A detail is one depth-4 heading followed by one question list.
- Guidance is the paragraphs and example lists that follow a heading immediately. The next heading or question list closes it.
- A question list is one unordered list. Every item is one paragraph whose only content is a bold label ending in a colon. No answer text is allowed in the template.
- An example list is one unordered list. No item may be a question. A question list and an example list are never the same list.
- Nothing follows a question list except the next legal heading: the next sibling, the next ancestor, or the end of the document.
- Thematic breaks, blockquotes, tables, code, raw HTML, task lists, and ordered lists are rejected.
- A deeper heading keeps the guidance of its parent headings. A heading of the same depth replaces only its own guidance.
- Every guidance item is kept. There is no sliding window and no silent truncation.

`ValidateQuestionnaire` is the schema gate. Template listing and compilation both call it before questions are extracted.

Example:

```markdown
# Character Backstory

Brief answers are sufficient.

## Core

- **Name:**
- **Age:**

## Hometown

Keep it local.

### Local Factions

A faction may be a family.

- a guild
- a temple

#### Faction 1

- **Name and type:**
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
