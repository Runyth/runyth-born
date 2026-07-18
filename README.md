# Template Wizard

`template-wizard` is a small, dependency-free terminal wizard for filling reusable Markdown questionnaires correctly.

The program reads a Markdown Abstract Syntax Tree (mdast) stored as JSON or YAML, discovers the questionnaire structure generically, guides the player through every recognised question, and continuously writes a normal filled Markdown file.

The same executable can therefore be reused with new or entirely different templates. Only the parsed template file needs to be replaced.

## What the player receives

Put these files in one folder:

- the executable for the player's operating system;
- one mdast template file ending in `.json`, `.yaml`, or `.yml`.

The player runs the executable. When there is exactly one valid template in the folder, it is selected automatically. When there are several, the player chooses one from a numbered list.

The resulting file is named `<template-name>-filled.md` by default. This is the only file the player needs to send to the DM.

A second local file ending in `.answers.json` is used to resume progress and merge matching answers into newer versions of the template. It does not need to be sent to the DM.

## Template workflow

Convert a Markdown template into raw mdast JSON with `markdown-rs-cli`:

```bash
markdown-rs-cli \
  --from markdown \
  --dialect gfm \
  backstory-template.md \
  --output backstory-template.mdast.json
```

Then distribute:

```text
template-wizard.exe
backstory-template.mdast.json
```

The executable does not invoke `markdown-rs-cli`; the parser is only needed by the template author when producing or updating the structured template file.

## AI-assisted template authoring

Every executable contains a canonical prompt that tells an AI exactly how to create a compatible Markdown source template:

```bash
template-wizard --ai-template-prompt
```

Copy the printed prompt into the AI assistant, replace the final purpose and language placeholders, and submit it. The prompt requires the AI to:

- output only the Markdown template;
- use headings for document structure;
- write every fillable question exactly as `- **Question label:**`;
- keep the final colon inside the bold label;
- leave every answer blank;
- avoid accidental question-like formatting in ordinary bullets;
- keep questions out of tables, blockquotes, code fences, HTML, and task lists;
- preserve stable headings and labels for answer merging across template versions.

The prompt is stored at `internal/prompt/template-authoring-prompt.md`, embedded into the binary at build time, and covered by tests. It therefore remains available even when only the standalone executable is distributed.

`markdown-rs-cli` documentation:

- https://crates.io/crates/markdown-rs-cli
- https://github.com/Akkitto/markdown-rs-cli

## Question convention

The surrounding Markdown structure is generic. Heading depth, explanatory paragraphs, blockquotes, nested lists, tables, code blocks, and other ordinary mdast content are preserved.

A fillable question is intentionally identified by one stable convention:

```markdown
- **Name:**
- **Age:**
- **Origin - What culture raised me?:**
```

In mdast terms, this is a list item whose first paragraph begins with a `strong` node and whose bold text ends in a colon.

The answer is always inserted after the strong node:

```markdown
- **Name:** Lilith
```

It is never placed inside the bold prompt:

```markdown
<!-- Incorrect and never generated -->
- **Name: Lilith**
```

Ordinary example bullets are not treated as questions unless they follow that convention.

See [TEMPLATE_FORMAT.md](TEMPLATE_FORMAT.md) for the exact rules and supported mdast nodes.

## Player controls

At each question, type an answer and press Enter. The active question is displayed in a dedicated high-contrast ASCII block immediately before the answer prompt, so it remains visually distinct from progress information, guidance, the existing answer, and command hints. Every question prompt also reminds the player that `/help` displays the available commands.

| Input | Behaviour |
|---|---|
| Left or Up arrow | Go to the previous question; remain on the first question at the top |
| Right or Down arrow | Go to the next question; remain on the last question at the bottom |
| `/p` | Go to the previous question |
| `/n` | Keep the current answer and continue |
| `/t` | Show a table of contents for all question sections |
| `/g NUMBER` | Jump to the numbered section shown by `/t` |
| `/goto NUMBER` | Jump to an exact question number |
| `/w` | Write the complete Markdown file and finish |
| `/q` | Quit after confirmation without performing another write |
| Empty line | Keep the current answer and continue |
| `/clear` | Clear the current answer and save immediately |
| `/multi` | Enter a multi-line answer; finish with a single `.` line |
| `/unanswered` | Jump to the next unanswered question |
| `/help` | Show the commands |

The longer existing command names remain supported, including `/back`, `/next`, `/skip`, `/finish`, and `/quit`.

Every committed answer still rewrites the Markdown output immediately using a temporary file and replacement operation. Consequently, `/q` does not discard earlier committed answers: it exits without an additional write after the user confirms. `/w` performs an explicit final full write and prints the completion summary.

Arrow-key navigation is enabled automatically when standard input is an interactive terminal. Repeated arrow presses stop at the first or last question and display the corresponding boundary message; they never finish the wizard. The command controls remain available in every terminal and when input is redirected.

## Command-line options

```text
Usage: template-wizard [options]

  --template, -t PATH
      Use a specific mdast JSON or YAML file.

  --output, -o PATH
      Write the filled Markdown to a specific path.

  --directory, -d PATH
      Search this directory for templates. Default: current directory.

  --restart
      Ignore saved answers and start from the current template.

  --list
      List valid templates and exit.

  --version
      Print the program version.

  --ai-template-prompt
      Print the bundled AI prompt for authoring compatible Markdown templates and exit.
```

Example:

```bash
./template-wizard-linux-amd64 \
  --template backstory-template.mdast.json \
  --output lilith-backstory.md
```

## Build locally

Requirements:

- Go 1.23 or newer.

Run tests:

```bash
go test ./...
```

Build the executable for the current system:

```bash
go build \
  -trimpath \
  -ldflags "-s -w" \
  -o template-wizard \
  ./cmd/template-wizard
```

Build all included release targets from Linux, macOS, or Windows:

```bash
./tools/build-all.sh
```

The script creates:

```text
dist/template-wizard-linux-amd64
dist/template-wizard-windows-amd64.exe
dist/template-wizard-macos-amd64
dist/template-wizard-macos-arm64
```

All builds use `CGO_ENABLED=0`, so the executables do not require a separately installed Go runtime or platform C libraries.

## Automated releases

The included GitHub Actions workflow tests the project and builds static executables for:

- Windows x86-64;
- Linux x86-64;
- macOS x86-64;
- macOS Apple Silicon.

Pushing a tag such as `v0.1.0` also uploads those binaries to the GitHub release.

The workflow deliberately disables `setup-go` dependency caching because the project uses only the Go standard library and therefore has no `go.sum` file.

## Deliberate boundaries

- This is a terminal wizard, not a graphical desktop application.
- It consumes raw mdast, not arbitrary home-grown JSON schemas.
- YAML support covers the JSON-compatible YAML normally produced by converting mdast JSON. YAML anchors, aliases, tags, and merge keys are rejected.
- Unknown mdast extension nodes are preserved as far as their generic `children` or `value` fields allow, but the renderer explicitly supports the standard mdast, GFM table/task-list, frontmatter, footnote, HTML, and math node families listed in `TEMPLATE_FORMAT.md`.
- The generated Markdown is canonical and structurally correct; it is not intended to reproduce the source template byte-for-byte.

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