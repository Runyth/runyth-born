package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Akkitto/template-wizard/internal/mdast"
)

func TestRunPrintsBundledAITemplatePromptWithoutTemplateFiles(t *testing.T) {
	directory := t.TempDir()

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--ai-template-prompt"},
		strings.NewReader(""),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}

	content := output.String()
	if !strings.Contains(content, "You are creating a reusable Markdown questionnaire template") {
		t.Fatalf("AI prompt introduction missing:\n%s", content)
	}
	if !strings.Contains(content, "- **Question label:**") {
		t.Fatalf("canonical question syntax missing:\n%s", content)
	}
	if strings.Contains(content, "no valid mdast") {
		t.Fatalf("prompt mode unexpectedly searched for templates:\n%s", content)
	}
}

func TestRunWritesMarkdownImmediatelyAndResumes(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		questionList("Name:", "Age:"),
	))

	var firstOutput bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("Lilith\n/q\ny\n"),
		&firstOutput,
		&firstOutput,
		"test",
	)
	if err != nil {
		t.Fatalf("first Run() error = %v\n%s", err, firstOutput.String())
	}

	markdownOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdownOutput), "- **Name:** Lilith") {
		t.Fatalf("output after first answer:\n%s", markdownOutput)
	}
	if strings.Contains(string(markdownOutput), "**Name: Lilith**") {
		t.Fatalf("answer was incorrectly bolded:\n%s", markdownOutput)
	}
	if !strings.Contains(firstOutput.String(), "No additional write was performed") {
		t.Fatalf("quit confirmation output:\n%s", firstOutput.String())
	}

	var secondOutput bytes.Buffer
	err = Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("95\n"),
		&secondOutput,
		&secondOutput,
		"test",
	)
	if err != nil {
		t.Fatalf("second Run() error = %v\n%s", err, secondOutput.String())
	}

	markdownOutput, err = os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdownOutput), "- **Name:** Lilith") || !strings.Contains(string(markdownOutput), "- **Age:** 95") {
		t.Fatalf("resumed output:\n%s", markdownOutput)
	}
}

func TestRunSupportsNavigationTableOfContentsSectionJumpAndWrite(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		heading(2, "1. Core"),
		questionList("Name:", "Age:"),
		heading(2, "2. Goals"),
		heading(3, "Immediate Goals"),
		questionList("Immediate need:", "Long-term dream:"),
	))

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("First name\n/p\nCorrected name\n/t\n/g 2\nFind a home\n/w\n"),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}

	markdownOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(markdownOutput)
	if !strings.Contains(content, "- **Name:** Corrected name") {
		t.Fatalf("previous navigation did not permit correction:\n%s", content)
	}
	if !strings.Contains(content, "- **Immediate need:** Find a home") {
		t.Fatalf("section jump did not reach section 2:\n%s", content)
	}
	if !strings.Contains(output.String(), "Table of Contents") || !strings.Contains(output.String(), "[2] 2. Goals") {
		t.Fatalf("table of contents was not displayed:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Finished") {
		t.Fatalf("/w did not perform the final write:\n%s", output.String())
	}
}

func TestRunSupportsArrowNavigationInFallbackInput(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		questionList("Name:", "Age:"),
	))

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("\x1b[C\n\x1b[D\nLilith\n/w\n"),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}

	markdownOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdownOutput), "- **Name:** Lilith") {
		t.Fatalf("arrow navigation did not return to the first question:\n%s", markdownOutput)
	}
}

func TestQuestionPromptIsVisuallyProminent(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		questionList("What does my character want?:"),
	))

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("/q\ny\n"),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}

	content := output.String()
	questionBlock := "============================================================\n" +
		"YOUR QUESTION\n" +
		"------------------------------------------------------------\n" +
		"What does my character want?:\n" +
		"============================================================"
	if !strings.Contains(content, questionBlock) {
		t.Fatalf("question was not rendered as a prominent block:\n%s", content)
	}

	questionPosition := strings.Index(content, "What does my character want?:")
	helpPosition := strings.Index(content, "Hint: type /help for information on commands.")
	if questionPosition < 0 || helpPosition < 0 || questionPosition > helpPosition {
		t.Fatalf("question should appear before the subordinate help hint:\n%s", content)
	}
}

func TestArrowNavigationStopsAtLastQuestionAndEveryPromptShowsHelpHint(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		questionList("Name:", "Age:"),
	))

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("\x1b[C\n\x1b[C\n/q\ny\n"),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}

	content := output.String()
	if !strings.Contains(content, "Already at the last question.") {
		t.Fatalf("repeated next-arrow navigation did not stop at the final question:\n%s", content)
	}
	if strings.Contains(content, "Finished\n========") {
		t.Fatalf("next-arrow navigation unexpectedly finished the wizard:\n%s", content)
	}
	if count := strings.Count(content, "Hint: type /help for information on commands."); count < 3 {
		t.Fatalf("help hint appeared %d times, want at least 3:\n%s", count, content)
	}
}

func TestQuitCanBeCancelled(t *testing.T) {
	directory := t.TempDir()
	templatePath := filepath.Join(directory, "character.mdast.json")
	outputPath := filepath.Join(directory, "character-filled.md")

	writeTemplate(t, templatePath, root(
		heading(1, "Character"),
		questionList("Name:", "Age:"),
	))

	var output bytes.Buffer
	err := Run(
		[]string{"--directory", directory, "--template", templatePath, "--output", outputPath},
		strings.NewReader("/q\nn\nLilith\n/w\n"),
		&output,
		&output,
		"test",
	)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Quit cancelled") {
		t.Fatalf("quit was not cancelled:\n%s", output.String())
	}

	markdownOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdownOutput), "- **Name:** Lilith") {
		t.Fatalf("answer after cancelled quit was not written:\n%s", markdownOutput)
	}
}

func TestBuildSectionsUsesMajorBreadcrumbAfterSharedDocumentTitle(t *testing.T) {
	questions := []mdast.Question{
		{Breadcrumb: []string{"Template", "1. Core"}, Answer: "Lilith"},
		{Breadcrumb: []string{"Template", "1. Core", "Origin"}},
		{Breadcrumb: []string{"Template", "5. Memories", "Memory 1"}, Answer: "A scar"},
	}

	sections := buildSections(questions)
	if len(sections) != 2 {
		t.Fatalf("len(sections) = %d, want 2: %#v", len(sections), sections)
	}
	if sections[0].Title != "1. Core" || sections[0].StartQuestion != 0 || sections[0].EndQuestion != 1 {
		t.Fatalf("first section = %#v", sections[0])
	}
	if sections[1].Title != "5. Memories" || sections[1].StartQuestion != 2 {
		t.Fatalf("second section = %#v", sections[1])
	}
	if target, ok := resolveSection("5", sections); !ok || target != 1 {
		t.Fatalf("resolveSection(5) = %d, %v; want 1, true", target, ok)
	}
}

func writeTemplate(t *testing.T, path string, template map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func root(nodes ...map[string]any) map[string]any {
	children := make([]any, 0, len(nodes))
	for _, node := range nodes {
		children = append(children, node)
	}
	return map[string]any{
		"type":     "root",
		"children": children,
	}
}

func heading(depth int, value string) map[string]any {
	return map[string]any{
		"type":  "heading",
		"depth": depth,
		"children": []any{
			map[string]any{"type": "text", "value": value},
		},
	}
}

func questionList(labels ...string) map[string]any {
	items := make([]any, 0, len(labels))
	for _, label := range labels {
		items = append(items, questionItem(label))
	}
	return map[string]any{
		"type":     "list",
		"ordered":  false,
		"spread":   false,
		"children": items,
	}
}

func questionItem(label string) map[string]any {
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
