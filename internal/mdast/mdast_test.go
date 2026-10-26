package mdast

import (
	"encoding/json"
	"testing"
)

func TestRejectTrailingJSON(t *testing.T) {
	for _, suffix := range []string{"{}", "garbage"} {
		if _, err := Parse([]byte(`{"type":"root","children":[]}`+suffix), ".json"); err == nil {
			t.Fatal("accepted trailing JSON content")
		}
	}
}

func TestQuestionExclusions(t *testing.T) {
	document := sampleDocument()
	list := document["children"].([]any)[3]
	document["children"] = []any{
		map[string]any{"type": "blockquote", "children": []any{list}},
		map[string]any{"type": "table", "children": []any{list}},
	}
	questions, err := ExtractQuestions(document)
	if err != nil || len(questions) != 0 {
		t.Fatalf("questions in excluded contexts: %v, %v", questions, err)
	}
	item := questionListItem("Task:")
	item["checked"] = false
	document["children"] = []any{map[string]any{"type": "list", "children": []any{item}}}
	questions, err = ExtractQuestions(document)
	if err != nil || len(questions) != 0 {
		t.Fatalf("task interpreted as question: %v, %v", questions, err)
	}
}

func TestMergeClearedDefaultAndAmbiguousLabels(t *testing.T) {
	document := sampleDocument()
	questions, _ := ExtractQuestions(document)
	if err := SetAnswer(document, questions[0], "Default"); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeAnswers(document, []Question{questions[0]}); err != nil {
		t.Fatal(err)
	}
	updated, _ := ExtractQuestions(document)
	if updated[0].Answer != "" {
		t.Fatal("cleared answer restored default")
	}
	// Both occurrences are ambiguous even though their ordinal fallback IDs differ.
	root := document["children"].([]any)
	root[3].(map[string]any)["children"] = []any{questionListItem("Name:"), questionListItem("Name:")}
	questions, _ = ExtractQuestions(document)
	saved := questions[0]
	saved.ID = "moved-section#1"
	saved.Answer = "Wrong answer"
	if _, err := MergeAnswers(document, []Question{saved}); err != nil {
		t.Fatal(err)
	}
	updated, _ = ExtractQuestions(document)
	if updated[0].Answer != "" || updated[1].Answer != "" {
		t.Fatal("ambiguous label fallback copied an answer")
	}
}

func TestParseSingleJSONRoundTrip(t *testing.T) {
	encoded, err := json.Marshal(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(encoded, ".json"); err != nil {
		t.Fatal(err)
	}
}

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
	if len(questions[0].Context) != 1 || guidancePlain(questions[0].Context[0].Inlines) != "Brief answers are enough." {
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
