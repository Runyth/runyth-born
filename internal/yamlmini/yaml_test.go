package yamlmini

import (
	"encoding/json"
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

func TestInlineJSONKeepsQuotedHashesInsideStrings(t *testing.T) {
	// Quoted hashes and comments around supported inline JSON collections
	// follow scalar quote semantics (F08).
	cases := map[string]any{
		"value: [\"a # b\"]":                 []any{"a # b"},
		"value: {\"note\": \"a # b\"}":       map[string]any{"note": "a # b"},
		"value: [\"a # b\"] # later":         []any{"a # b"},
		"value: {\"k\": [1, \"x # y\"]} # c": map[string]any{"k": []any{json.Number("1"), "x # y"}},
		"value: [\"a \\\" b # c\"]":          []any{"a \" b # c"},
		"value: [\"# not comment\"]":         []any{"# not comment"},
		"value: {}":                          map[string]any{},
		"value: []":                          []any{},
	}
	for source, want := range cases {
		parsed, err := Parse([]byte(source + "\n"))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
		mapping, ok := parsed.(map[string]any)
		if !ok {
			t.Fatalf("root type = %T, want map[string]any", parsed)
		}
		got := mapping["value"]
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("value = %#v, want %#v", got, want)
		}
	}
	for _, source := range []string{"value: [\"just text\"] trailing\n", "value: [\"multi\n"} {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted invalid inline JSON source %q", source)
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

// wantDocument is one reference-comparison case: the document text, the
// wanted value, or a rejection.
type wantDocument struct {
	source string
	value  any
}

// assertWantDocument compares Parse output against the reference value after
// a JSON round trip so numeric encodings stay out of the comparison; a
// reference value of nil requires a rejection.
func assertWantDocument(t *testing.T, doc wantDocument) {
	t.Helper()
	parsed, err := Parse([]byte(doc.source))
	if doc.value == nil {
		if err == nil {
			t.Fatalf("accepted %q", doc.source)
		}
		return
	}
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", doc.source, err)
	}
	parsedBytes, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("Marshal(%q) error = %v", doc.source, err)
	}
	wantBytes, err := json.Marshal(doc.value)
	if err != nil {
		t.Fatalf("Marshal reference for %q error = %v", doc.source, err)
	}
	if string(parsedBytes) != string(wantBytes) {
		t.Fatalf("Parse(%q) = %s, want %s", doc.source, parsedBytes, wantBytes)
	}
}

func TestEmptyValueBlockDispatchMatchesReference(t *testing.T) {
	// F08: a comment-only or absent value dispatches to the nested block at
	// any deeper indentation, and a bare plain scalar folds across lines.
	for _, doc := range []wantDocument{
		{"value:\n  x\n\n", map[string]any{"value": "x"}},
		{"value:\n  x\n", map[string]any{"value": "x"}},
		{"value:\n  x\n y\n", map[string]any{"value": "x y"}},
		{"value:\n     ke y: # h\n", map[string]any{"value": map[string]any{"ke y": nil}}},
		{"value:\n   k: beta\n     a,b\n", map[string]any{"value": map[string]any{"k": "beta a,b"}}},
		{"value:\n  z: # h\n\n   {\"a\": \"b: c # d\"}\n", map[string]any{"value": map[string]any{"z": map[string]any{"a": "b: c # d"}}}},
		{"value:\n    # h\n    - x\n      y\n", map[string]any{"value": []any{"x y"}}},
		{"value:\n  true\n", map[string]any{"value": true}},
		{"value:\n  null\n", map[string]any{"value": nil}},
		{"value:\n     12\n", map[string]any{"value": 12}},
		{"value:\n  \"d\"\n", map[string]any{"value": "d"}},
		{"value:\n  {\"a\": 1}\n", map[string]any{"value": map[string]any{"a": 1}}},
		{"value:\n  - item\n", map[string]any{"value": []any{"item"}}},
		{"value:\n- item\n", map[string]any{"value": []any{"item"}}},
		{"k:\n  |-\n    text\n", map[string]any{"k": "text"}},
		{"k:\n      |-\n        text\n", map[string]any{"k": "text"}},
		{"k:\n  |-2\n       x\n", map[string]any{"k": "     x"}},
		{"k:\n  |2\n     x\n", map[string]any{"k": "   x\n"}},
		{"value:\n  x\n- y\n", nil},
		{"a:\n  b:\n- x\n", nil},
		{"value:\n      a: 1\n        b: 2\n", nil},
	} {
		assertWantDocument(t, doc)
	}
}

func TestMultilinePlainScalarsFoldLikeReference(t *testing.T) {
	// F08: blank runs fold to line breaks, deeper lines continue with a
	// single space, and trailing comments end the fold.
	for _, doc := range []wantDocument{
		{"k: 12\n  2\n", map[string]any{"k": "12 2"}},
		{"k: 12\n\n  b\n", map[string]any{"k": "12\nb"}},
		{"value:\n\n    true\n", map[string]any{"value": true}},
		{"value:\n        \n   null\n    # h\n", map[string]any{"value": nil}},
		{"value:\n  \n    null\n   \n  null\n", map[string]any{"value": "null\nnull"}},
		{"value:\n  true\n\n    a,b\n", map[string]any{"value": "true\na,b"}},
		{"value:\n    12\n    ['q', 12] # c\n", map[string]any{"value": "12 ['q', 12]"}},
		{"x\ny\n", "x y"},
		{"x\n y\n", "x y"},
		{"-\n  x\n  y\n", []any{"x y"}},
		{"-\n  x\n y\n", []any{"x y"}},
		{"-\n  x\n- y\n", []any{"x", "y"}},
	} {
		assertWantDocument(t, doc)
	}
	for _, source := range []string{
		"value:\n  x # c\n    y\n",
		"k: 12 # c\n  y\n",
		"k: 1 # c\n  beta\n",
	} {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted comment-truncated fold %q", source)
		}
	}
}

func TestPlainScalarValuesUnfoldableSeparatorsAreRejected(t *testing.T) {
	// F08: a plain scalar value with its own separate value indicator is a
	// scanner error in the reference parser.
	for _, doc := range []wantDocument{
		{"k: a: b\n", nil},
		{"value:\n  ke y: a: b\n", nil},
		{"value:\n    z: y: 2\n", nil},
		{"value:\n   k: beta\n  a : b\n", nil},
		{"- a: b: c\n", nil},
		{"- : x\n", nil},
	} {
		assertWantDocument(t, doc)
	}
	for _, doc := range []wantDocument{
		{"k: 'a: b'\n", map[string]any{"k": "a: b"}},
		{"k: \"a: b\"\n", map[string]any{"k": "a: b"}},
		{"- a: b\n", []any{map[string]any{"a": "b"}}},
		{"k: a:b\n", map[string]any{"k": "a:b"}},
		{"- ::x\n", []any{"::x"}},
	} {
		assertWantDocument(t, doc)
	}
}

func TestFlowCollectionsMatchReferenceGrammar(t *testing.T) {
	// F08: the reference flow grammar covers bare keys, shorthand entries,
	// trailing commas, single-quote escaping, unsigned values, and rejects
	// indicators that cannot start a node.
	for _, doc := range []wantDocument{
		{"{a: 1}\n", map[string]any{"a": 1}},
		{"{x}\n", map[string]any{"x": nil}},
		{"[x]\n", []any{"x"}},
		{"{a: 1, b: [x, 2]}\n", map[string]any{"a": 1, "b": []any{"x", 2}}},
		{"{\"a\": 1, }\n", map[string]any{"a": 1}},
		{"[1, ]\n", []any{1}},
		{"[a: b]\n", []any{map[string]any{"a": "b"}}},
		{"[a: b, c: d]\n", []any{map[string]any{"a": "b"}, map[string]any{"c": "d"}}},
		{"[a, 'b: c']\n", []any{"a", "b: c"}},
		{"{a: 'b''c'}\n", map[string]any{"a": "b'c"}},
		{"{a: \"}\"}\n", map[string]any{"a": "}"}},
		{"{a: \"\\u00e9\"}\n", map[string]any{"a": "é"}},
		{"[a\"b]\n", []any{"a\"b"}},
		{"{a\"b: c}\n", map[string]any{"a\"b": "c"}},
		{"[a,]\n", []any{"a"}},
		{"[a: , b]\n", []any{map[string]any{"a": nil}, "b"}},
		{"[a:]\n", []any{map[string]any{"a": nil}}},
		{"[a, b]\n", []any{"a", "b"}},
		{"{a: yes}\n", map[string]any{"a": true}},
		{"[a\" b] # c\n", []any{"a\" b"}},
	} {
		assertWantDocument(t, doc)
	}
	for _, source := range []string{
		"{,}\n",
		"[:, v]\n",
		"[: v]\n",
		"{a: b: c}\n",
		"{'a'\n",
		"{#c}\n",
		"[# c]\n",
		"[|x]\n",
		"[>x]\n",
		"{a: @x}\n",
		"[a: b]\n\n c\n",
	} {
		if _, err := Parse([]byte(source)); err == nil {
			t.Fatalf("accepted invalid flow collection %q", source)
		}
	}
}

func TestRepeatedMappingKeysKeepTheLastValue(t *testing.T) {
	// F08: the reference constructor silently overwrites repeated keys.
	for _, doc := range []wantDocument{
		{"a: b\na: b\n", map[string]any{"a": "b"}},
		{"k:\n  a: 1\n  a: 2\n", map[string]any{"k": map[string]any{"a": 2}}},
		{"a: 1\nb: 2\na: true\n", map[string]any{"a": true, "b": 2}},
		{"{a: 1, a: true}\n", map[string]any{"a": true}},
	} {
		assertWantDocument(t, doc)
	}
}

func TestBlockScalarValuesAtArbitraryDeeperIndentation(t *testing.T) {
	// F08: nested blocks are valid at any indentation deeper than their
	// parent, validated against the reference parser semantics.
	for _, doc := range []wantDocument{
		{"value:\n    a: 1\n    b: 2\n", map[string]any{"value": map[string]any{"a": 1, "b": 2}}},
		{"value:\n   k: [1, 2]\n", map[string]any{"value": map[string]any{"k": []any{1, 2}}}},
		{"value:\n  {a:, b: 2}\n", map[string]any{"value": map[string]any{"a": nil, "b": 2}}},
	} {
		assertWantDocument(t, doc)
	}
}
