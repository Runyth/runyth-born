package prompt

import (
	_ "embed"
	"strings"
)

// templateAuthoring contains the canonical AI instructions for creating a
// Markdown source template that Template Wizard can recognise after mdast
// conversion.
//
//go:embed template-authoring-prompt.md
var templateAuthoring string

// TemplateAuthoring returns the bundled prompt with exactly one trailing
// newline, ready to print or copy into an AI assistant.
func TemplateAuthoring() string {
	return strings.TrimSpace(templateAuthoring) + "\n"
}
