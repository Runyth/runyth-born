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

// A complete valid mdast template may author inline JSON children with
// quoted hashes inside string values (F08).
func TestCompileInlineJSONWithQuotedHash(t *testing.T) {
	for _, structure := range []string{`"Hash # identity:"`, `"Name:"`} {
		source := []byte(`type: root
children:
  - type: heading
    depth: 1
    children:
      - type: text
        value: Inline Hash Template
  - type: heading
    depth: 2
    children:
      - type: text
        value: Core
  - type: list
    children:
      - type: listItem
        spread: false
        checked: null
        children:
          - type: paragraph
            children: [{"type": "strong", "children": [{"type": "text", "value": ` + structure + `}]}]
`)
		compiled, err := Compile(source, ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled.Questions) == 0 {
			t.Fatal("no questions")
		}
		want := "**Hash \\# identity:**"
		if structure == `"Name:"` {
			want = "**Name:**"
		}
		if !strings.Contains(compiled.Parts[0].Text, want) {
			t.Fatalf("inline JSON content changed: %q", compiled.Parts[0].Text)
		}
	}
}

// A complete valid mdast template may author multiline values with an
// explicit indentation indicator and varying line depths; this compiles end
// to end (F08).
func TestCompileTemplateWithExplicitIndentationBlockScalar(t *testing.T) {
	source := []byte(`type: root
children:
  - type: heading
    depth: 1
    children:
      - type: text
        value: Block Scalar Template
  - type: paragraph
    children:
      - type: text
        value: |2-
          Brief answers are sufficient.
            Explicit depths count too.
  - type: heading
    depth: 2
    children:
      - type: text
        value: Core
  - type: list
    ordered: false
    spread: false
    children:
      - type: listItem
        spread: false
        checked: null
        children:
          - type: paragraph
            children:
              - type: strong
                children:
                  - type: text
                    value: "Name:"
`)
	compiled, err := Compile(source, ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Questions) == 0 {
		t.Fatal("no questions")
	}
	if !strings.Contains(compiled.Parts[0].Text, "  Explicit depths count too\\.") {
		t.Fatalf("explicit indentation content not preserved: %q", compiled.Parts[0].Text)
	}
}

// A complete valid mdast template may author sequence children and nested
// blocks at any indentation deeper than their parent, not only the plus-two
// steps (F08).
func TestCompileTemplateWithArbitraryChildrenIndentation(t *testing.T) {
	source := []byte(`type: root
children:
    - type: heading
      depth: 1
      children:
           - type: text
             value: Arbitrary Indent Template
    - type: paragraph
      children:
           - type: text
             value: Brief answers are sufficient.
    - type: heading
      depth: 2
      children:
           - type: text
             value: Core
    - type: list
      spread: false
      children:
             - type: listItem
               spread: false
               checked: null
               children:
                    - type: paragraph
                      children:
                           - type: strong
                             children:
                                  - type: text
                                    value: "Name:"
`)
	compiled, err := Compile(source, ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Questions) == 0 {
		t.Fatal("no questions")
	}
	if !strings.Contains(compiled.Parts[0].Text, "Name:") {
		t.Fatalf("arbitrary indentation content not preserved: %q", compiled.Parts[0].Text)
	}
}

// A complete valid mdast template may quote its first mapping key. That is a
// mapping entry, not a standalone quoted scalar (F08).
func TestCompileTemplateWithQuotedRootKey(t *testing.T) {
	source := []byte(`"type": root
children:
    - type: heading
      depth: 1
      children:
           - type: text
             value: Quoted Key Template
    - type: heading
      depth: 2
      children:
           - type: text
             value: Core
    - type: list
      ordered: false
      start: null
      spread: false
      children:
             - type: listItem
               spread: false
               checked: null
               children:
                    - type: paragraph
                      children:
                           - type: strong
                             children:
                                  - type: text
                                    value: "Name:"
`)
	compiled, err := Compile(source, ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Questions) == 0 {
		t.Fatal("no questions")
	}
	if !strings.Contains(compiled.Parts[0].Text, "Name:") {
		t.Fatalf("quoted root key content not preserved: %q", compiled.Parts[0].Text)
	}
}
