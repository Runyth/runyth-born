package markdown

import (
	"strings"
	"testing"

	"github.com/Akkitto/template-wizard-web/internal/mdast"
)

func TestRenderPlacesAnswerOutsideBoldQuestion(t *testing.T) {
	document := mdast.Document{
		"type": "root",
		"children": []any{
			map[string]any{
				"type":  "heading",
				"depth": 1,
				"children": []any{
					map[string]any{"type": "text", "value": "Character"},
				},
			},
			map[string]any{
				"type":    "list",
				"ordered": false,
				"spread":  false,
				"children": []any{
					map[string]any{
						"type": "listItem",
						"children": []any{
							map[string]any{
								"type": "paragraph",
								"children": []any{
									map[string]any{
										"type": "strong",
										"children": []any{
											map[string]any{"type": "text", "value": "Name:"},
										},
									},
									map[string]any{"type": "text", "value": " Lilith *the Swift*"},
								},
							},
						},
					},
				},
			},
		},
	}

	output, err := Render(document)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(output, "- **Name:** Lilith \\*the Swift\\*") {
		t.Fatalf("output does not contain expected question/answer:\n%s", output)
	}
	if strings.Contains(output, "**Name: Lilith") {
		t.Fatalf("answer was placed inside bold label:\n%s", output)
	}
}

func TestRenderNestedListAndMultilineAnswer(t *testing.T) {
	document := mdast.Document{
		"type": "root",
		"children": []any{
			map[string]any{
				"type":    "list",
				"ordered": false,
				"spread":  false,
				"children": []any{
					map[string]any{
						"type": "listItem",
						"children": []any{
							map[string]any{
								"type": "paragraph",
								"children": []any{
									map[string]any{"type": "text", "value": "Parent"},
								},
							},
							map[string]any{
								"type":    "list",
								"ordered": false,
								"spread":  false,
								"children": []any{
									map[string]any{
										"type": "listItem",
										"children": []any{
											map[string]any{
												"type": "paragraph",
												"children": []any{
													map[string]any{"type": "text", "value": "Child"},
													map[string]any{"type": "break"},
													map[string]any{"type": "text", "value": "Second line"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	output, err := Render(document)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "- Parent\n  - Child  \n    Second line") {
		t.Fatalf("unexpected nested list output:\n%s", output)
	}
}

func TestLiteralTextCannotBecomeMarkdownStructure(t *testing.T) {
	input := "_literal_ `code`\n# heading\n- list"
	want := "\\_literal\\_ \\`code\\`\n\\# heading\n\\- list"
	if got := escapeText(input, inlineContext{}); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := renderInlineCode("   "); got != "`   `" {
		t.Fatalf("space-only inline code changed: %q", got)
	}
}
