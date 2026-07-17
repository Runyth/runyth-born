# Template Format

The complete AI authoring prompt can be printed from any built executable:

```bash
template-wizard --ai-template-prompt
```

Its source is embedded from `internal/prompt/template-authoring-prompt.md`.

## Input root

The input must be a raw mdast document with a root object resembling:

```json
{
  "type": "root",
  "children": []
}
```

The program accepts the same object structure in JSON-compatible YAML.

## Recognised questions

A question must be represented by a list item whose first block is a paragraph and whose first meaningful inline node is bold text ending in a colon.

Example Markdown:

```markdown
- **Name:**
```

Relevant mdast shape:

```json
{
  "type": "listItem",
  "children": [
    {
      "type": "paragraph",
      "children": [
        {
          "type": "strong",
          "children": [
            {
              "type": "text",
              "value": "Name:"
            }
          ]
        }
      ]
    }
  ]
}
```

An existing answer may already follow the bold node:

```json
{
  "type": "text",
  "value": " Lilith"
}
```

The wizard replaces only the answer portion. The bold prompt itself is preserved.

## Structure and guidance

The scanner tracks headings from depth 1 through 6. They become the breadcrumb shown above each question.

Explanatory paragraphs and ordinary non-question bullets in the current section become guidance text. This lets the template retain instructions such as:

```markdown
### Local Problems

Add one to three small problems suitable for a level 1-3 party.

- missing livestock;
- thefts or smuggling;

- **Problem 1:**
```

## Stable answer merging

The local answer-state file records each answer by:

1. heading breadcrumb;
2. question label;
3. occurrence number.

When a new template version changes a section heading, the wizard may fall back to a globally unique label and occurrence match. This allows many template revisions to retain existing answers.

Changing a question label intentionally makes it a new question unless the old and new labels still match through the fallback rules.

## Explicitly rendered block nodes

- `root`
- `heading`
- `paragraph`
- `thematicBreak`
- `blockquote`
- `list`
- `listItem`
- `code`
- `html`
- `definition`
- `footnoteDefinition`
- `table`
- `tableRow`
- `tableCell`
- `yaml`
- `math`

## Explicitly rendered inline nodes

- `text`
- `strong`
- `emphasis`
- `delete`
- `inlineCode`
- `break`
- `link`
- `linkReference`
- `image`
- `imageReference`
- `footnoteReference`
- `html`
- `inlineMath`

Unknown container nodes fall back to rendering their `children`. Unknown literal nodes fall back to their `value` string.

## YAML subset

The built-in YAML reader supports:

- indentation-based mappings;
- indentation-based sequences;
- plain, single-quoted, and double-quoted scalars;
- booleans, null, integers, and floating-point values;
- inline JSON arrays and objects;
- literal `|` and folded `>` block scalars;
- document markers.

It intentionally rejects:

- anchors;
- aliases;
- custom tags;
- merge keys;
- tab indentation.

JSON remains the preferred distribution format because it is exactly what `markdown-rs-cli` emits and has less ambiguity.
