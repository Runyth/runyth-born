package staticapp

import (
	"os"
	"strings"
	"testing"
)

func TestCompileTemplateSlots(t *testing.T) {
	for _, extension := range []string{"json", "yaml"} {
		source, err := os.ReadFile("../../examples/minimal-template.mdast." + extension)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := Compile(source, "."+extension)
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled.Questions) == 0 {
			t.Fatal("no questions")
		}
		slots := 0
		for _, part := range compiled.Parts {
			if part.Question == nil {
				continue
			}
			if *part.Question != slots || !strings.Contains(part.Continuation, "\n") {
				t.Fatalf("invalid part: %#v", part)
			}
			slots++
		}
		if slots != len(compiled.Questions) {
			t.Fatal("missing answer slots")
		}
	}
}
