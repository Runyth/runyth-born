package templates

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Akkitto/template-wizard-web/internal/mdast"
)

type Entry struct {
	ID            string
	Name          string
	Filename      string
	Path          string
	Fingerprint   string
	QuestionCount int
	SectionCount  int
}

type Catalog struct {
	directory string
}

func New(directory string) *Catalog {
	return &Catalog{directory: directory}
}

func (catalog *Catalog) Directory() string {
	return catalog.directory
}

func (catalog *Catalog) List() ([]Entry, error) {
	if err := os.MkdirAll(catalog.directory, 0o755); err != nil {
		return nil, fmt.Errorf("create templates directory: %w", err)
	}

	directoryEntries, err := os.ReadDir(catalog.directory)
	if err != nil {
		return nil, fmt.Errorf("read templates directory: %w", err)
	}

	result := make([]Entry, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() || directoryEntry.Type()&os.ModeSymlink != 0 {
			continue
		}
		extension := strings.ToLower(filepath.Ext(directoryEntry.Name()))
		if extension != ".json" && extension != ".yaml" && extension != ".yml" {
			continue
		}

		path := filepath.Join(catalog.directory, directoryEntry.Name())
		entry, err := catalog.loadEntry(path)
		if err != nil {
			return nil, fmt.Errorf("load template %q: %w", directoryEntry.Name(), err)
		}
		result = append(result, entry)
	}

	sort.Slice(result, func(left int, right int) bool {
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	return result, nil
}

func (catalog *Catalog) Get(id string) (Entry, mdast.Document, error) {
	entries, err := catalog.List()
	if err != nil {
		return Entry{}, nil, err
	}
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		document, err := mdast.LoadFile(entry.Path)
		if err != nil {
			return Entry{}, nil, err
		}
		return entry, document, nil
	}
	return Entry{}, nil, os.ErrNotExist
}

func (catalog *Catalog) GetByFilename(filename string) (Entry, mdast.Document, error) {
	cleanFilename := filepath.Base(filename)
	if cleanFilename != filename {
		return Entry{}, nil, os.ErrNotExist
	}
	path := filepath.Join(catalog.directory, cleanFilename)
	entry, err := catalog.loadEntry(path)
	if err != nil {
		return Entry{}, nil, err
	}
	document, err := mdast.LoadFile(path)
	if err != nil {
		return Entry{}, nil, err
	}
	return entry, document, nil
}

func (catalog *Catalog) loadEntry(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	if !info.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("template is not a regular file")
	}

	document, err := mdast.LoadFile(path)
	if err != nil {
		return Entry{}, err
	}
	questions, err := mdast.ExtractQuestions(document)
	if err != nil {
		return Entry{}, err
	}
	if len(questions) == 0 {
		return Entry{}, fmt.Errorf("template contains no recognised questions")
	}
	fingerprint, err := mdast.Fingerprint(document)
	if err != nil {
		return Entry{}, err
	}

	filename := filepath.Base(path)
	name := firstHeading(document)
	if name == "" {
		name = humaniseFilename(filename)
	}

	return Entry{
		ID:            stableID(filename),
		Name:          name,
		Filename:      filename,
		Path:          path,
		Fingerprint:   fingerprint,
		QuestionCount: len(questions),
		SectionCount:  countSections(questions),
	}, nil
}

func stableID(filename string) string {
	digest := sha256.Sum256([]byte(filename))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
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

func humaniseFilename(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, suffix := range []string{".mdast", "-mdast", "_mdast"} {
		name = strings.TrimSuffix(name, suffix)
	}
	name = strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(name)
	words := strings.Fields(name)
	for index, word := range words {
		if word == "" {
			continue
		}
		letters := []rune(word)
		letters[0] = unicode.ToUpper(letters[0])
		words[index] = string(letters)
	}
	return strings.Join(words, " ")
}

func countSections(questions []mdast.Question) int {
	previous := ""
	count := 0
	for _, question := range questions {
		current := sectionName(question)
		if current != previous {
			count++
			previous = current
		}
	}
	return count
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
