package staticapp

import (
	"strings"
	"testing"

	"github.com/Akkitto/template-wizard-web/internal/mdast"
)

func TestAnalyzeAndReconcileAnswers(t *testing.T) {
	t.Parallel()

	source := []byte(`{"type":"root","children":[{"type":"heading","depth":1,"children":[{"type":"text","value":"Example"}]},{"type":"heading","depth":2,"children":[{"type":"text","value":"Core"}]},{"type":"list","ordered":false,"children":[{"type":"listItem","children":[{"type":"paragraph","children":[{"type":"strong","children":[{"type":"text","value":"Name:"}]}]}]}]}]}`)

	initial, err := Analyze(source, ".json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if initial.TemplateName != "Example" || len(initial.Questions) != 1 || len(initial.Sections) != 1 {
		t.Fatalf("unexpected analysis: %#v", initial)
	}

	saved := append([]mdast.Question(nil), initial.Questions...)
	saved[0].Answer = "Akito"
	reconciled, err := Analyze(source, ".json", saved)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Questions[0].Answer != "Akito" {
		t.Fatalf("answer was not merged: %#v", reconciled.Questions[0])
	}
	if !strings.Contains(reconciled.Markdown, "**Name:** Akito") {
		t.Fatalf("rendered Markdown does not contain answer:\n%s", reconciled.Markdown)
	}
}
