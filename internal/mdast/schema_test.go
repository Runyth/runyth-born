package mdast

import (
	"os"
	"strings"
	"testing"
)

func TestQuestionnaireSchemaRejectsAQuestionListFollowedByProse(t *testing.T) {
	document := schemaDocument(`# Example

## Core

- **Name:**

This belongs before the questions.
`)
	err := ValidateQuestionnaire(document)
	if err == nil || !strings.Contains(err.Error(), "question list") {
		t.Fatalf("error = %v", err)
	}
}

func TestQuestionnaireSchemaRejectsASkippedHeadingDepth(t *testing.T) {
	document := schemaDocument(`# Example

## Hometown

#### Location 1

- **Name:**
`)
	err := ValidateQuestionnaire(document)
	if err == nil || !strings.Contains(err.Error(), "depth-3") {
		t.Fatalf("error = %v", err)
	}
}

func TestParentGuidanceSurvivesADeeperHeading(t *testing.T) {
	document := schemaDocument(`# Example

Write this once.

## Getting Started

Think of one small thing.

- an NPC
- a shrine

### My Contribution

- **The thing:**
`)
	if err := ValidateQuestionnaire(document); err != nil {
		t.Fatal(err)
	}
	questions, err := ExtractQuestions(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 {
		t.Fatalf("questions = %d", len(questions))
	}
	if guidancePlain(questions[0].Context[0].Inlines) != "Write this once." {
		t.Fatalf("preamble was not shown on the first question: %#v", questions[0].Context)
	}
	joined := plainGuidance(questions[0].Context)
	for _, want := range []string{"Think of one small thing.", "an NPC", "a shrine"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("context = %#v, missing %q", questions[0].Context, want)
		}
	}
}

func TestSiblingHeadingReplacesOnlyItsOwnGuidance(t *testing.T) {
	document := schemaDocument(`# Example

## Hometown

Keep it local.

### Local Factions

A faction may be a family.

#### Faction 1

- **Name and type:**

### Local Rumours

Rumours may be false.

- **Rumour 1:**
`)
	if err := ValidateQuestionnaire(document); err != nil {
		t.Fatal(err)
	}
	questions, err := ExtractQuestions(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 {
		t.Fatalf("questions = %#v", questions)
	}
	faction := plainGuidance(questions[0].Context)
	if !strings.Contains(faction, "Keep it local.") || !strings.Contains(faction, "A faction may be a family.") {
		t.Fatalf("faction context = %#v", questions[0].Context)
	}
	rumour := plainGuidance(questions[1].Context)
	if strings.Contains(rumour, "A faction may be a family.") || !strings.Contains(rumour, "Keep it local.") || !strings.Contains(rumour, "Rumours may be false.") {
		t.Fatalf("rumour context = %#v", questions[1].Context)
	}
}

func TestPublishedBackstoryMatchesTheQuestionnaireSchema(t *testing.T) {
	raw, err := os.ReadFile("../../templates/player-character-backstory.mdast.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := Parse(raw, ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuestionnaire(document); err != nil {
		t.Fatal(err)
	}
	questions, err := ExtractQuestions(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 150 {
		t.Fatalf("question count = %d", len(questions))
	}
	if !strings.Contains(plainGuidance(questions[0].Context), "EVERYTHING IS OPTIONAL!") {
		t.Fatalf("preamble missing from the first question: %#v", questions[0].Context)
	}
	if !citationHasStrong(questions[0].Context, "Important to me") {
		t.Fatalf("citation styling missing: %#v", questions[0].Context)
	}
	started := questionByLabel(t, questions, "The thing")
	if !strings.Contains(plainGuidance(started.Context), "Think of one small thing") {
		t.Fatalf("getting started context = %#v", started.Context)
	}
	faction := questionByLabel(t, questions, "Name and type")
	if !strings.Contains(plainGuidance(faction.Context), "A faction may be a family") {
		t.Fatalf("faction context = %#v", faction.Context)
	}
	if !strings.Contains(plainGuidance(faction.Context), "Keep it local and playable.") {
		t.Fatalf("hometown guidance missing: %#v", faction.Context)
	}
}

func citationHasStrong(blocks []GuidanceBlock, text string) bool {
	for _, block := range blocks {
		if block.Type != "blockquote" {
			continue
		}
		for _, part := range block.Blocks {
			for _, inline := range part.Inlines {
				if inline.Strong && inline.Text == text {
					return true
				}
			}
		}
	}
	return false
}

func plainGuidance(blocks []GuidanceBlock) string {
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type == "blockquote" {
			builder.WriteString(plainGuidance(block.Blocks))
			continue
		}
		for _, inline := range block.Inlines {
			builder.WriteString(inline.Text)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func questionByLabel(t *testing.T, questions []Question, label string) Question {
	t.Helper()
	for _, question := range questions {
		if question.Label == label {
			return question
		}
	}
	t.Fatalf("missing question %q", label)
	return Question{}
}

func schemaDocument(markdown string) Document {
	t := testingTB{}
	document, err := parseSchemaMarkdown(markdown)
	if err != nil {
		t.fatal(err)
	}
	return document
}

type testingTB struct{}

func (testingTB) fatal(err error) {
	panic(err)
}

func parseSchemaMarkdown(source string) (Document, error) {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	children := make([]any, 0)
	index := 0
	for index < len(lines) {
		line := strings.TrimRight(lines[index], " ")
		if line == "" {
			index++
			continue
		}
		if strings.HasPrefix(line, "#") {
			depth := 0
			for depth < len(line) && line[depth] == '#' {
				depth++
			}
			children = append(children, headingNode(depth, strings.TrimSpace(line[depth:])))
			index++
			continue
		}
		if strings.HasPrefix(line, "- **") && strings.HasSuffix(line, ":**") {
			items := make([]any, 0)
			for index < len(lines) && strings.HasPrefix(strings.TrimRight(lines[index], " "), "- **") {
				label := strings.TrimSpace(strings.TrimPrefix(strings.TrimRight(lines[index], " "), "- "))
				items = append(items, questionListItem(strings.Trim(label, "*")))
				index++
			}
			children = append(children, map[string]any{"type": "list", "ordered": false, "children": items})
			continue
		}
		if strings.HasPrefix(line, "- ") {
			items := make([]any, 0)
			for index < len(lines) && strings.HasPrefix(strings.TrimRight(lines[index], " "), "- ") {
				items = append(items, map[string]any{
					"type": "listItem",
					"children": []any{map[string]any{
						"type":     "paragraph",
						"children": []any{map[string]any{"type": "text", "value": strings.TrimSpace(lines[index][2:])}},
					}},
				})
				index++
			}
			children = append(children, map[string]any{"type": "list", "ordered": false, "children": items})
			continue
		}
		children = append(children, map[string]any{
			"type":     "paragraph",
			"children": []any{map[string]any{"type": "text", "value": strings.TrimSpace(line)}},
		})
		index++
	}
	return Document{"type": "root", "children": children}, nil
}

func headingNode(depth int, title string) map[string]any {
	return map[string]any{
		"type":     "heading",
		"depth":    depth,
		"children": []any{map[string]any{"type": "text", "value": title}},
	}
}
