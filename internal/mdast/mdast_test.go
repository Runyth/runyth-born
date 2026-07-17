package mdast

import "testing"

func TestExtractAndSetAnswers(t *testing.T) {
	document := sampleDocument()

	questions, err := ExtractQuestions(document)
	if err != nil {
		t.Fatalf("ExtractQuestions() error = %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("question count = %d, want 2", len(questions))
	}
	if questions[0].Label != "Name" {
		t.Fatalf("first label = %q", questions[0].Label)
	}
	if got := questions[0].Breadcrumb; len(got) != 2 || got[0] != "Character Template" || got[1] != "Core" {
		t.Fatalf("breadcrumb = %#v", got)
	}
	if len(questions[0].Context) != 1 || questions[0].Context[0] != "Brief answers are enough." {
		t.Fatalf("context = %#v", questions[0].Context)
	}

	if err := SetAnswer(document, questions[0], "Lilith"); err != nil {
		t.Fatalf("SetAnswer() error = %v", err)
	}
	if err := SetAnswer(document, questions[1], "First line\nSecond line"); err != nil {
		t.Fatalf("SetAnswer() multiline error = %v", err)
	}

	updated, err := ExtractQuestions(document)
	if err != nil {
		t.Fatalf("ExtractQuestions() after update error = %v", err)
	}
	if updated[0].Answer != "Lilith" {
		t.Fatalf("first answer = %q", updated[0].Answer)
	}
	if updated[1].Answer != "First line\nSecond line" {
		t.Fatalf("second answer = %q", updated[1].Answer)
	}
}

func TestMergeAnswersAcrossMovedSection(t *testing.T) {
	oldDocument := sampleDocument()
	oldQuestions, _ := ExtractQuestions(oldDocument)
	if err := SetAnswer(oldDocument, oldQuestions[0], "Lilith"); err != nil {
		t.Fatal(err)
	}
	saved, _ := ExtractQuestions(oldDocument)

	newDocument := sampleDocument()
	rootChildren := newDocument["children"].([]any)
	section := rootChildren[1].(map[string]any)
	section["children"] = []any{map[string]any{"type": "text", "value": "Identity"}}

	merged, err := MergeAnswers(newDocument, saved)
	if err != nil {
		t.Fatalf("MergeAnswers() error = %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged = %d, want 1", merged)
	}
	questions, _ := ExtractQuestions(newDocument)
	if questions[0].Answer != "Lilith" {
		t.Fatalf("merged answer = %q", questions[0].Answer)
	}
}

func sampleDocument() Document {
	return Document{
		"type": "root",
		"children": []any{
			map[string]any{
				"type":  "heading",
				"depth": 1,
				"children": []any{
					map[string]any{"type": "text", "value": "Character Template"},
				},
			},
			map[string]any{
				"type":  "heading",
				"depth": 2,
				"children": []any{
					map[string]any{"type": "text", "value": "Core"},
				},
			},
			map[string]any{
				"type": "paragraph",
				"children": []any{
					map[string]any{"type": "text", "value": "Brief answers are enough."},
				},
			},
			map[string]any{
				"type":    "list",
				"ordered": false,
				"spread":  false,
				"children": []any{
					questionListItem("Name:"),
					questionListItem("Reason to stay:"),
					map[string]any{
						"type": "listItem",
						"children": []any{
							map[string]any{
								"type": "paragraph",
								"children": []any{
									map[string]any{"type": "text", "value": "This is an example, not a question."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func questionListItem(label string) map[string]any {
	return map[string]any{
		"type": "listItem",
		"children": []any{
			map[string]any{
				"type": "paragraph",
				"children": []any{
					map[string]any{
						"type": "strong",
						"children": []any{
							map[string]any{"type": "text", "value": label},
						},
					},
				},
			},
		},
	}
}
