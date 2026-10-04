package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogListsValidTemplatesAndIgnoresOtherFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	valid := `{"type":"root","children":[{"type":"heading","depth":1,"children":[{"type":"text","value":"Example"}]},{"type":"list","ordered":false,"children":[{"type":"listItem","children":[{"type":"paragraph","children":[{"type":"strong","children":[{"type":"text","value":"Name:"}]}]}]}]}]}`
	if err := os.WriteFile(filepath.Join(directory, "example.mdast.json"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := New(directory).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one template, got %d", len(entries))
	}
	if entries[0].Name != "Example" || entries[0].QuestionCount != 1 {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

func TestCatalogRejectsInvalidSupportedTemplate(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "broken.yaml"), []byte("type: root\nchildren: ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := New(directory).List(); err == nil {
		t.Fatal("expected invalid supported template to fail catalog generation")
	}
}

func TestHumaniseUnicodeFilename(t *testing.T) {
	if got := humaniseFilename("épreuve-du-héros.mdast.json"); got != "Épreuve Du Héros" {
		t.Fatalf("corrupted Unicode filename: %q", got)
	}
}
