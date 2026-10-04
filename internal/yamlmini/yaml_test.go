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
