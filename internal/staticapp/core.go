package staticapp

import (
	"fmt"
	"strings"

	markdownrenderer "github.com/Runyth/runyth-born/internal/markdown"
	"github.com/Runyth/runyth-born/internal/mdast"
)

type Section struct {
	Index    int    `json:"index"`
	Title    string `json:"title"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Answered int    `json:"answered"`
	Total    int    `json:"total"`
}

type Result struct {
	TemplateName string           `json:"template_name"`
	Fingerprint  string           `json:"fingerprint"`
	Questions    []mdast.Question `json:"questions"`
	Sections     []Section        `json:"sections"`
	Markdown     string           `json:"markdown,omitempty"`
}

func Analyze(source []byte, extension string, saved []mdast.Question) (Result, error) {
	document, err := mdast.Parse(source, extension)
	if err != nil {
		return Result{}, err
	}
	if err := mdast.ValidateQuestionnaire(document); err != nil {
		return Result{}, err
	}

	fingerprint, err := mdast.Fingerprint(document)
	if err != nil {
		return Result{}, err
	}

	if len(saved) > 0 {
		if _, err := mdast.MergeAnswers(document, saved); err != nil {
			return Result{}, fmt.Errorf("merge saved answers: %w", err)
		}
	}

	questions, err := mdast.ExtractQuestions(document)
	if err != nil {
		return Result{}, err
	}
	if len(questions) == 0 {
		return Result{}, fmt.Errorf("template contains no recognised questions")
	}

	rendered, err := markdownrenderer.Render(document)
	if err != nil {
		return Result{}, err
	}

	return Result{
		TemplateName: firstHeading(document),
		Fingerprint:  fingerprint,
		Questions:    questions,
		Sections:     BuildSections(questions),
		Markdown:     rendered,
	}, nil
}

func BuildSections(questions []mdast.Question) []Section {
	result := make([]Section, 0)
	for questionIndex, question := range questions {
		title := sectionName(question)
		if len(result) == 0 || result[len(result)-1].Title != title {
			result = append(result, Section{
				Index: len(result) + 1,
				Title: title,
				Start: questionIndex,
				End:   questionIndex,
			})
		}

		current := &result[len(result)-1]
		current.End = questionIndex
		current.Total++
		if strings.TrimSpace(question.Answer) != "" {
			current.Answered++
		}
	}
	return result
}

func FirstUnanswered(questions []mdast.Question) int {
	for index, question := range questions {
		if strings.TrimSpace(question.Answer) == "" {
			return index
		}
	}
	return 0
}

func firstHeading(document mdast.Document) string {
	children, ok := document["children"].([]any)
	if !ok {
		return ""
	}
	for _, child := range children {
		node, ok := child.(map[string]any)
		if !ok || node["type"] != "heading" {
			continue
		}
		return strings.TrimSpace(mdast.PlainText(node))
	}
	return ""
}

func sectionName(question mdast.Question) string {
	switch len(question.Breadcrumb) {
	case 0:
		return "Questions"
	case 1:
		return question.Breadcrumb[0]
	default:
		return question.Breadcrumb[1]
	}
}
