package yamlmini

import (
	"reflect"
	"testing"
)

func TestParseMdastYAML(t *testing.T) {
	source := []byte(`type: root
children:
  - type: heading
    depth: 1
    children:
      - type: text
        value: "Character Template"
  - type: paragraph
    children:
      - type: text
        value: |-
          First line.
          Second line.
  - type: list
    ordered: false
    start: null
    spread: false
    children: []
`)

	parsed, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	root, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("root type = %T, want map[string]any", parsed)
	}
	if root["type"] != "root" {
		t.Fatalf("root type field = %#v", root["type"])
	}

	children, ok := root["children"].([]any)
	if !ok || len(children) != 3 {
		t.Fatalf("children = %#v", root["children"])
	}

	paragraph := children[1].(map[string]any)
	text := paragraph["children"].([]any)[0].(map[string]any)
	if text["value"] != "First line.\nSecond line." {
		t.Fatalf("block scalar = %#v", text["value"])
	}

	list := children[2].(map[string]any)
	if !reflect.DeepEqual(list["children"], []any{}) {
		t.Fatalf("inline empty list = %#v", list["children"])
	}
}

func TestRejectsAnchors(t *testing.T) {
	_, err := Parse([]byte("type: &kind root\nchildren: []\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want unsupported anchor error")
	}
}

func TestSequenceMappingCannotPanic(t *testing.T) {
	_, err := Parse([]byte("- __yamlmini_impossible__: allowed\n  type: root\n"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestRejectsMergeKeysAndTrailingInlineJSON(t *testing.T) {
	for _, source := range []string{"- <<: {}\n", "children: [] trailing\n", "children: [broken\n"} {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted invalid input %q", source)
		}
	}
}

func TestBlockScalarHeaderAndFoldingSemantics(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"explicit indentation indicator", "value: |2-\n    Indented\n    text\n", "  Indented\n  text"},
		{"folded blank line", "value: >-\n  first\n\n  second\n", "first\nsecond"},
		{"folded more-indented lines", "value: >-\n  first\n    nested\n  second\n", "first\n  nested\nsecond"},
		{"comment after literal header", "value: | # note\n  first\n", "first\n"},
		{"comment after folded header", "value: >- # note\n  first\n", "first"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed, err := Parse([]byte(testCase.source))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			mapping, ok := parsed.(map[string]any)
			if !ok {
				t.Fatalf("root type = %T, want map[string]any", parsed)
			}
			if mapping["value"] != testCase.want {
				t.Fatalf("value = %q, want %q", mapping["value"], testCase.want)
			}
		})
	}
}

func TestBlockScalarReferenceBehavior(t *testing.T) {
	// Every case was cross-checked against PyYAML 6.0.2 and the YAML 1.2.2
	// scalar rules (F08).
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"explicit indentation with varying depths", "value: |2-\n    first\n  second\n", "  first\nsecond"},
		{"explicit indentation with more-deep content", "value: |2\n    a\n      b\n", "  a\n    b\n"},
		{"explicit folded with blank unrelated run", "value: >2\n    a\n\n      b\n", "  a\n\n    b\n"},
		{"unterminated final line under clip", "value: |\n  a", "a"},
		{"unterminated two-line literal", "value: |\n  a\n  b", "a\nb"},
		{"unterminated folded", "value: >\n  a\n  b", "a b"},
		{"keep with final blank real", "value: |+\n  a\n\n", "a\n\n"},
		{"keep with significant spaces line", "value: |+\n  a\n    \n", "a\n  \n"},
		{"keep with bigger significant spaces", "value: |+\n  a\n      \n", "a\n    \n"},
		{"clip keeps significant spaces", "value: |\n  a\n    \n", "a\n  \n"},
		{"strip keeps significant spaces", "value: |-\n  a\n    \n", "a\n  "},
		{"blank spaces inside literal", "value: |\n  a\n    \n  b\n", "a\n  \nb\n"},
		{"folded keeps space line as literal", "value: >\n  a\n    \n  b\n", "a\n  \nb\n"},
		{"folded strip no final newline", "value: >-\n  a", "a"},
		{"keep without final newline", "value: |+\n  a", "a"},
		{"blank-only clip", "value: |\n \n", ""},
		{"blank-only keep", "value: |+\n \n\n", "\n\n"},
		{"unterminated deep blank under keep", "value: |+\n  a\n  ", "a\n"},
		{"unterminated deep content under keep", "value: |+\n  a\n    ", "a\n  "},
		{"literal small blank below indent", "value: |+\n  a\n \n", "a\n\n"},
		{"folded blank run under keep", "value: >+\n  a\n\n\n", "a\n\n\n"},
		{"literal blank run under keep", "value: |+\n  a\n\n\n", "a\n\n\n"},
		{"folded MI blank MI", "value: >-\n  a\n    b\n\n    c\n", "a\n  b\n\n  c"},
		{"literal tab inside content", "value: |\n  a\n  \tb\n", "a\n\tb\n"},
		{"leading blanks contribute", "value: >-\n\n  a\n", "\na"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed, err := Parse([]byte(testCase.source))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			mapping, ok := parsed.(map[string]any)
			if !ok {
				t.Fatalf("root type = %T, want map[string]any", parsed)
			}
			if mapping["value"] != testCase.want {
				t.Fatalf("value = %q, want %q", mapping["value"], testCase.want)
			}
		})
	}
}

func TestRejectedBlockScalarsMatchReference(t *testing.T) {
	// Invalid headers and structural tabs are rejected outright instead of
	// being accepted with altered content; validated against PyYAML (F08).
	sources := []string{
		"value: |2\u0662\n  a\n",
		"value: |\t\n  a\n",
		"value: |2\t# note\n  a\n",
		"value: ||\n",
		"value: >>\n  a\n  b\n",
		"value: |x\n  a\n",
		"value: |2x-3\n  a\n",
		"value: |0\n",
		"a: 1\n\t \n",
		"a: \t1\n",
		"a: x\ty\n",
		"a: 'x'\tb\n",
		"- \t a\n",
		"a:\n  b: 1\t2\n",
	}
	for _, source := range sources {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted invalid input %q", source)
		}
	}
}

func TestPlainScalarWithApostropheKeepsCommentHandling(t *testing.T) {
	parsed, err := Parse([]byte("value: don't know # note\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	mapping, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("root type = %T, want map[string]any", parsed)
	}
	if mapping["value"] != "don't know" {
		t.Fatalf("value = %q, want %q", mapping["value"], "don't know")
	}
}

func TestBlockScalarChompingAndBlankSemantics(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"keep with terminating artifact", "value: |+\n  a\n", "a\n"},
		{"keep with blank-only content", "value: |+\n\n", "\n"},
		{"keep with real trailing blank", "value: |+\n  a\n\n", "a\n\n"},
		{"folded blank before more-indented line", "value: >-\n  a\n\n    b\n", "a\n\n  b"},
		{"folded two blanks before more-indented line", "value: >-\n  a\n\n\n    b\n", "a\n\n\n  b"},
		{"folded keep with blank run", "value: >+\n  a\n    b\n  c\n\n  d\n", "a\n  b\nc\nd\n"},
		{"explicit indentation with keep", "value: |2+\n    a\n", "  a\n"},
		{"literal preserves blank runs", "value: |\n  a\n\n\n  b\n", "a\n\n\nb\n"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed, err := Parse([]byte(testCase.source))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			mapping, ok := parsed.(map[string]any)
			if !ok {
				t.Fatalf("root type = %T, want map[string]any", parsed)
			}
			if mapping["value"] != testCase.want {
				t.Fatalf("value = %q, want %q", mapping["value"], testCase.want)
			}
		})
	}
}

func TestMalformedBlockScalarHeadersAreRejected(t *testing.T) {
	for _, source := range []string{"value: |0\n", "value: |x\n  a\n", "value: |2x-3\n  a\n"} {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted a malformed block scalar header %q", source)
		}
	}
}

func TestQuotedScalarsKeepCommentCharactersInsideTheScalar(t *testing.T) {
	for source, want := range map[string]string{
		"value: \"a # b\"\n":      "a # b",
		"value: 'it''s # here'\n": "it's # here",
	} {
		parsed, err := Parse([]byte(source))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
		mapping, ok := parsed.(map[string]any)
		if !ok {
			t.Fatalf("root type = %T, want map[string]any", parsed)
		}
		if mapping["value"] != want {
			t.Fatalf("value = %q, want %q", mapping["value"], want)
		}
	}
}

func TestPlainMappingKeyWithApostropheParses(t *testing.T) {
	parsed, err := Parse([]byte("don't: 1\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	mapping, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("root type = %T, want map[string]any", parsed)
	}
	if mapping["don't"] != int64(1) {
		t.Fatalf("value = %#v", mapping["don't"])
	}
}

func TestBlockScalarInSequenceItemIsRejected(t *testing.T) {
	if _, err := Parse([]byte("- |\n  x\n")); err == nil {
		t.Fatal("accepted a block scalar inside a sequence item")
	}
	if _, err := Parse([]byte("- |2-\n    x\n")); err == nil {
		t.Fatal("accepted an indented block scalar inside a sequence item")
	}
}
