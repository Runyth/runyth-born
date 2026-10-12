package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Runyth/runyth-born/internal/staticapp"
	templatecatalog "github.com/Runyth/runyth-born/internal/templates"
)

type catalogDocument struct {
	SchemaVersion int            `json:"schema_version"`
	AppVersion    string         `json:"app_version"`
	Templates     []catalogEntry `json:"templates"`
}

type catalogEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Filename      string `json:"filename"`
	URL           string `json:"url"`
	Extension     string `json:"extension"`
	Fingerprint   string `json:"fingerprint"`
	QuestionCount int    `json:"question_count"`
	SectionCount  int    `json:"section_count"`
}

func main() {
	templatesDir := flag.String("templates", "templates", "directory containing mdast templates")
	outputPath := flag.String("output", "dist/templates/catalog.json", "catalog output path")
	appVersion := flag.String("version", "dev", "application version")
	flag.Parse()

	entries, err := templatecatalog.New(*templatesDir).List()
	if err != nil {
		fail(err)
	}

	output := catalogDocument{
		SchemaVersion: 1,
		AppVersion:    strings.TrimSpace(*appVersion),
		Templates:     make([]catalogEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Filename, "catalog.json") || strings.HasSuffix(strings.ToLower(entry.Filename), ".compiled.json") {
			fail(fmt.Errorf("reserved template filename %q", entry.Filename))
		}
		source, err := os.ReadFile(entry.Path)
		if err != nil {
			fail(err)
		}
		compiled, err := staticapp.Compile(source, filepath.Ext(entry.Filename))
		if err != nil {
			fail(fmt.Errorf("compile %s: %w", entry.Filename, err))
		}
		compiledName := entry.Filename + ".compiled.json"
		if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
			fail(err)
		}
		encoded, err := json.Marshal(compiled)
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(*outputPath), compiledName), append(encoded, '\n'), 0o644); err != nil {
			fail(err)
		}
		output.Templates = append(output.Templates, catalogEntry{
			ID:            entry.ID,
			Name:          entry.Name,
			Filename:      entry.Filename,
			URL:           "./templates/" + url.PathEscape(compiledName),
			Extension:     strings.ToLower(filepath.Ext(entry.Filename)),
			Fingerprint:   entry.Fingerprint,
			QuestionCount: entry.QuestionCount,
			SectionCount:  entry.SectionCount,
		})
	}
	sort.Slice(output.Templates, func(left, right int) bool {
		return strings.ToLower(output.Templates[left].Name) < strings.ToLower(output.Templates[right].Name)
	})

	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		fail(err)
	}
	file, err := os.Create(*outputPath)
	if err != nil {
		fail(err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		_ = file.Close()
		fail(err)
	}
	if err := file.Close(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "template catalog: %v\n", err)
	os.Exit(1)
}
