You are creating a reusable Markdown questionnaire template for **Runyth Born**.

Runyth Born does not read the Markdown source directly. The template author will first convert the Markdown to raw mdast JSON or YAML with `markdown-rs-cli`, then commit that parsed template to the repository's `templates/` directory. The static browser wizard discovers fillable questions from the mdast structure and writes each player's answers into a normal Markdown file.

Create the requested template while obeying every rule below.

## Required output

1. Output only the finished Markdown template.
2. Do not wrap the complete document in a code fence.
3. Do not add commentary before or after the template.
4. Produce valid UTF-8 GitHub-Flavoured Markdown.
5. Leave every fillable answer blank.

## Document structure

The document must match this grammar exactly. The build rejects every other shape.

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

- Start with one `#` title.
- Put document-wide notes in paragraphs immediately after the title and before the first `##` chapter. Those paragraphs are the preamble. They are shown with the first question only.
- A `##` chapter contains either one question list or one or more `###` subchapters. It cannot contain both.
- A `###` subchapter contains either one question list or one or more `####` details. It cannot contain both.
- A `####` detail contains guidance, if any, and then exactly one question list.
- Do not use heading depths 5 or 6. Do not skip a depth.
- Put each heading's explanation immediately after that heading, before its question list or its first child heading. A deeper heading keeps that explanation. A heading of the same depth replaces only its own explanation.
- Do not put anything after a question list except the next legal heading.
- Do not use thematic breaks, blockquotes, tables, code fences, raw HTML, task lists, or ordered lists.
- Keep heading names and question labels stable between template versions whenever their meaning has not changed. This allows saved answers to merge into updated templates.
- Prefer unique, descriptive question labels. Repeated labels are supported under different headings, but unnecessary duplication makes template updates less reliable.

## Exact fillable-question syntax

Every fillable question must be a single ordinary unordered-list item in exactly this structural form:

```markdown
- **Question label:**
```

The following details are mandatory:

- Start the list item with `- `.
- The first content in the list item must be the bold question label.
- Put the entire label in `**bold text**`.
- Put the final colon inside the bold text.
- Close the bold text immediately after that colon.
- Put no answer, placeholder, instruction, or other text after the closing `**`.
- Use one question per list item.
- Keep the question item to one paragraph.
- Do not use task-list checkboxes for questions.

Valid questions:

```markdown
- **Name:**
- **Age:**
- **Origin - What culture or community raised me?:**
```

Invalid questions:

```markdown
- **Name:** Your answer here
- **Name: Your answer here**
- **Name**:
- Name:
**Name:**
- [ ] **Name:**
- Before the label **Name:**
```

Runyth Born writes the answer after the bold label, producing this filled result:

```markdown
- **Name:** Lilith
```

It must never need to repair a question whose answer was placed inside the bold text.

## Preventing accidental questions

Any list item whose first meaningful content is bold text ending in a colon is treated as fillable. Therefore:

- Do not format ordinary example bullets, warnings, labels, or glossary entries as bold text ending in a colon.
- For non-fillable bullets, use plain text, or ensure their initial bold text does not end in a colon.
- Put guidance in the heading's guidance block, before its question list or its first child heading.
- Do not place fillable questions inside tables, blockquotes, code fences, raw HTML, or plain paragraphs. They must use the exact list-item form above.

Safe guidance and examples:

```markdown
Choose any entries that are relevant.

Examples include:

- a childhood saying;
- a professional habit;
- a local superstition.

- **A habit from my upbringing:**
- **A belief I rejected:**
```

## Designing answerable templates

- The wizard accepts free-text answers, including optional multiline answers. Do not rely on specialised form controls, radio buttons, checkboxes, or dropdowns.
- When several separate answers are needed, create separate fillable questions.
- When a question is optional, explain that in nearby prose; keep the question syntax unchanged.
- When users may add more entries, provide a reasonable number of repeated, clearly named slots such as `NPC 1`, `NPC 2`, and `NPC 3`, or explain that they may use a multiline answer.
- Keep instructions concise and place them before the questions they explain.
- Do not pre-fill sample answers in fillable question items. Put examples in surrounding prose instead.

## Compatibility check

Before returning the document, silently verify all of the following:

- Every intended question is written as `- **Label:**` with the colon inside the bold text.
- Every intended question is blank after the closing `**`.
- No ordinary bullet accidentally matches the question convention.
- Questions are not placed in tables, blockquotes, code fences, HTML, or task lists.
- The document matches the grammar above, including heading depth and the placement of every guidance block.
- The complete response contains only the Markdown template.

## Template to create

Purpose and content:

[PASTE THE DESCRIPTION OF THE DESIRED TEMPLATE HERE]

Output language:

[STATE THE LANGUAGE, OR WRITE "same as the request"]
