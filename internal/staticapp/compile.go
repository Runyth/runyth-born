package staticapp

import (
	"fmt"
	"strings"

	"github.com/Runyth/runyth-born/internal/markdown"
	"github.com/Runyth/runyth-born/internal/mdast"
)

// CompiledTemplate stores layout and question metadata for the plain JS client.
type CompiledTemplate struct {
	SchemaVersion int              `json:"schema_version"`
	TemplateName  string           `json:"template_name"`
	Fingerprint   string           `json:"fingerprint"`
	Questions     []mdast.Question `json:"questions"`
	Parts         []MarkdownPart   `json:"parts"`
}

type MarkdownPart struct {
	Text         string `json:"text,omitempty"`
	Question     *int   `json:"question,omitempty"`
	Continuation string `json:"continuation,omitempty"`
}

func Compile(source []byte, extension string) (CompiledTemplate, error) {
	analysis, err := Analyze(source, extension, nil)
	if err != nil {
		return CompiledTemplate{}, err
	}
	document, err := mdast.Parse(source, extension)
	if err != nil {
		return CompiledTemplate{}, err
	}
	questions, err := mdast.ExtractQuestions(document)
	if err != nil {
		return CompiledTemplate{}, err
	}
	// Alphanumeric markers survive Markdown escaping; avoid content collisions.
	marker := "RB" + analysis.Fingerprint
	for strings.Contains(analysis.Markdown, marker) {
		marker += "X"
	}
	for index, question := range questions {
		answer := fmt.Sprintf("%sQ%dA\n%sQ%dB", marker, index, marker, index)
		if err := mdast.SetAnswer(document, question, answer); err != nil {
			return CompiledTemplate{}, err
		}
	}
	rendered, err := markdown.Render(document)
	if err != nil {
		return CompiledTemplate{}, err
	}
	parts := make([]MarkdownPart, 0, len(questions)*2+1)
	for index := range questions {
		start := fmt.Sprintf("%sQ%dA", marker, index)
		end := fmt.Sprintf("%sQ%dB", marker, index)
		position := strings.Index(rendered, " "+start)
		if position < 0 {
			return CompiledTemplate{}, fmt.Errorf("cannot compile answer slot %d", index)
		}
		parts = append(parts, MarkdownPart{Text: rendered[:position]})
		rendered = rendered[position+1+len(start):]
		position = strings.Index(rendered, end)
		if position < 0 {
			return CompiledTemplate{}, fmt.Errorf("cannot compile continuation slot %d", index)
		}
		questionIndex := index
		parts = append(parts, MarkdownPart{Question: &questionIndex, Continuation: rendered[:position]})
		rendered = rendered[position+len(end):]
	}
	parts = append(parts, MarkdownPart{Text: rendered})
	return CompiledTemplate{
		SchemaVersion: 1, TemplateName: analysis.TemplateName,
		Fingerprint: analysis.Fingerprint, Questions: analysis.Questions, Parts: parts,
	}, nil
}
