package prompt

import (
	"strings"
	"testing"
)

func TestTemplateAuthoringContainsCanonicalQuestionRules(t *testing.T) {
	value := TemplateAuthoring()

	required := []string{
		"- **Question label:**",
		"Put the final colon inside the bold text.",
		"Put no answer, placeholder, instruction, or other text after the closing `**`.",
		"Do not wrap the complete document in a code fence.",
		"[PASTE THE DESCRIPTION OF THE DESIRED TEMPLATE HERE]",
	}

	for _, fragment := range required {
		if !strings.Contains(value, fragment) {
			t.Fatalf("TemplateAuthoring() does not contain %q", fragment)
		}
	}

	if !strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\n\n") {
		t.Fatalf("TemplateAuthoring() must end with exactly one newline")
	}
}
