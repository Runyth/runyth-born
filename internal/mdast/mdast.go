package mdast

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Akkitto/template-wizard-web/internal/yamlmini"
)

type Document map[string]any

type Question struct {
	Path         []int    `json:"-"`
	Label        string   `json:"label"`
	Breadcrumb   []string `json:"breadcrumb"`
	Context      []string `json:"context,omitempty"`
	Answer       string   `json:"answer"`
	ID           string   `json:"id"`
	FallbackID   string   `json:"fallback_id"`
	LabelOrdinal int      `json:"label_ordinal"`
}

func LoadFile(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %q: %w", path, err)
	}

	document, err := Parse(data, filepath.Ext(path))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", path, err)
	}
	return document, nil
}

// Parse decodes and validates one mdast document from JSON or the supported
// JSON-compatible YAML subset. The extension must be .json, .yaml, or .yml.
func Parse(data []byte, extension string) (Document, error) {
	extension = strings.ToLower(strings.TrimSpace(extension))
	var decoded any
	var err error

	switch extension {
	case ".json", "json":
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("decode JSON template: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("JSON template must contain exactly one document")
		}
	case ".yaml", ".yml", "yaml", "yml":
		decoded, err = yamlmini.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("decode YAML template: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported template extension %q; expected .json, .yaml, or .yml", extension)
	}

	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("template does not contain an mdast root object")
	}

	document := Document(root)
	if err := Validate(document); err != nil {
		return nil, fmt.Errorf("invalid mdast template: %w", err)
	}
	return document, nil
}

func Validate(document Document) error {
	if nodeType(document) != "root" {
		return fmt.Errorf("top-level mdast node must have type \"root\"")
	}
	if _, ok := children(document); !ok {
		return fmt.Errorf("top-level mdast root must contain a children array")
	}
	return nil
}

func Discover(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", directory, err)
	}

	candidates := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".json" && extension != ".yaml" && extension != ".yml" {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		if _, err := LoadFile(path); err == nil {
			candidates = append(candidates, path)
		}
	}

	sort.Slice(candidates, func(left int, right int) bool {
		return strings.ToLower(filepath.Base(candidates[left])) < strings.ToLower(filepath.Base(candidates[right]))
	})

	return candidates, nil
}

func Clone(document Document) (Document, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("clone mdast document: %w", err)
	}

	var cloned Document
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		return nil, fmt.Errorf("clone mdast document: %w", err)
	}
	return cloned, nil
}

func Fingerprint(document Document) (string, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("fingerprint mdast document: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ExtractQuestions(document Document) ([]Question, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}

	scanner := &questionScanner{
		headings:      make([]string, 6),
		labelOrdinals: map[string]int{},
		exactOrdinals: map[string]int{},
	}
	scanner.walk(map[string]any(document), nil, false)
	return scanner.questions, nil
}

func SetAnswer(document Document, question Question, answer string) error {
	paragraph, err := nodeAtPath(document, question.Path)
	if err != nil {
		return err
	}
	if nodeType(paragraph) != "paragraph" {
		return fmt.Errorf("question path no longer points to a paragraph")
	}

	inlineChildren, ok := children(paragraph)
	if !ok {
		return fmt.Errorf("question paragraph does not contain children")
	}

	strongIndex := firstQuestionStrongIndex(inlineChildren)
	if strongIndex < 0 {
		return fmt.Errorf("question paragraph no longer begins with a bold label ending in a colon")
	}

	updated := append([]any(nil), inlineChildren[:strongIndex+1]...)
	if answer != "" {
		lines := strings.Split(normalizeNewlines(answer), "\n")
		updated = append(updated, map[string]any{
			"type":  "text",
			"value": " " + lines[0],
		})
		for _, line := range lines[1:] {
			updated = append(updated,
				map[string]any{"type": "break"},
				map[string]any{"type": "text", "value": line},
			)
		}
	}

	paragraph["children"] = updated
	return nil
}

func MergeAnswers(document Document, saved []Question) (int, error) {
	current, err := ExtractQuestions(document)
	if err != nil {
		return 0, err
	}

	byID := make(map[string]Question, len(saved))
	fallbackSaved := map[string][]Question{}
	fallbackCurrent := map[string]int{}
	for _, answer := range saved {
		byID[answer.ID] = answer
		key := fallbackLabel(answer)
		fallbackSaved[key] = append(fallbackSaved[key], answer)
	}
	for _, question := range current {
		fallbackCurrent[fallbackLabel(question)]++
	}

	merged := 0
	for _, question := range current {
		selected, ok := byID[question.ID]
		key := fallbackLabel(question)
		if !ok && fallbackCurrent[key] == 1 && len(fallbackSaved[key]) == 1 {
			selected = fallbackSaved[key][0]
			ok = true
		}
		if !ok {
			continue
		}
		if err := SetAnswer(document, question, selected.Answer); err != nil {
			return merged, err
		}
		if selected.Answer != "" {
			merged++
		}
	}

	return merged, nil
}

func fallbackLabel(question Question) string {
	if index := strings.LastIndex(question.FallbackID, "#"); index >= 0 {
		return question.FallbackID[:index]
	}
	return question.FallbackID
}

func PlainText(node map[string]any) string {
	switch nodeType(node) {
	case "text", "inlineCode", "code", "html", "yaml", "math", "inlineMath":
		return stringValue(node, "value")
	case "image":
		return stringValue(node, "alt")
	case "break":
		return "\n"
	}

	values, ok := children(node)
	if !ok {
		return ""
	}

	var builder strings.Builder
	for _, child := range values {
		childNode, ok := child.(map[string]any)
		if !ok {
			continue
		}
		builder.WriteString(PlainText(childNode))
	}
	return builder.String()
}

type questionScanner struct {
	questions     []Question
	headings      []string
	context       []string
	labelOrdinals map[string]int
	exactOrdinals map[string]int
}

func (scanner *questionScanner) walk(node map[string]any, path []int, insideListItem bool) {
	switch nodeType(node) {
	case "blockquote", "table", "code", "html", "footnoteDefinition":
		return
	case "heading":
		depth := intValue(node, "depth", 1)
		if depth < 1 {
			depth = 1
		}
		if depth > 6 {
			depth = 6
		}
		title := cleanText(PlainText(node))
		scanner.headings[depth-1] = title
		for index := depth; index < len(scanner.headings); index++ {
			scanner.headings[index] = ""
		}
		scanner.context = nil
		return

	case "paragraph":
		if !insideListItem {
			text := cleanText(PlainText(node))
			if text != "" {
				scanner.addContext(text)
			}
		}
		return

	case "listItem":
		if _, checked := node["checked"].(bool); checked {
			return
		}
		if paragraph, paragraphPath, ok := questionParagraph(node, path); ok {
			scanner.addQuestion(paragraph, paragraphPath)

			nodeChildren, _ := children(node)
			for index, child := range nodeChildren {
				if index == 0 {
					continue
				}
				childNode, ok := child.(map[string]any)
				if ok {
					scanner.walk(childNode, appendPath(path, index), true)
				}
			}
			return
		}

		text := cleanText(PlainText(node))
		if text != "" && !containsQuestion(node) {
			scanner.addContext("- " + text)
		}
	}

	nodeChildren, ok := children(node)
	if !ok {
		return
	}
	nextInsideListItem := insideListItem || nodeType(node) == "listItem"
	for index, child := range nodeChildren {
		childNode, ok := child.(map[string]any)
		if !ok {
			continue
		}
		scanner.walk(childNode, appendPath(path, index), nextInsideListItem)
	}
}

func (scanner *questionScanner) addQuestion(paragraph map[string]any, path []int) {
	paragraphChildren, _ := children(paragraph)
	strongIndex := firstQuestionStrongIndex(paragraphChildren)
	strong, _ := paragraphChildren[strongIndex].(map[string]any)
	label := cleanText(PlainText(strong))
	label = strings.TrimSpace(strings.TrimSuffix(label, ":"))

	breadcrumb := compactStrings(scanner.headings)
	exactBase := normalizeID(strings.Join(breadcrumb, " > ") + "\x1f" + label)
	scanner.exactOrdinals[exactBase]++
	exactOrdinal := scanner.exactOrdinals[exactBase]

	fallbackBase := normalizeID(label)
	scanner.labelOrdinals[fallbackBase]++
	labelOrdinal := scanner.labelOrdinals[fallbackBase]

	answer := answerAfterStrong(paragraphChildren, strongIndex)
	scanner.questions = append(scanner.questions, Question{
		Path:         append([]int(nil), path...),
		Label:        label,
		Breadcrumb:   append([]string(nil), breadcrumb...),
		Context:      append([]string(nil), scanner.context...),
		Answer:       answer,
		ID:           exactBase + "#" + strconv.Itoa(exactOrdinal),
		FallbackID:   fallbackBase + "#" + strconv.Itoa(labelOrdinal),
		LabelOrdinal: labelOrdinal,
	})
}

func (scanner *questionScanner) addContext(text string) {
	text = cleanText(text)
	if text == "" {
		return
	}
	scanner.context = append(scanner.context, text)
	const maximumContextItems = 12
	if len(scanner.context) > maximumContextItems {
		scanner.context = append([]string(nil), scanner.context[len(scanner.context)-maximumContextItems:]...)
	}
}

func questionParagraph(listItem map[string]any, listItemPath []int) (map[string]any, []int, bool) {
	values, ok := children(listItem)
	if !ok || len(values) == 0 {
		return nil, nil, false
	}
	paragraph, ok := values[0].(map[string]any)
	if !ok || nodeType(paragraph) != "paragraph" {
		return nil, nil, false
	}
	paragraphChildren, ok := children(paragraph)
	if !ok || firstQuestionStrongIndex(paragraphChildren) < 0 {
		return nil, nil, false
	}
	return paragraph, appendPath(listItemPath, 0), true
}

func firstQuestionStrongIndex(values []any) int {
	for index, value := range values {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if nodeType(node) == "text" && strings.TrimSpace(stringValue(node, "value")) == "" {
			continue
		}
		if nodeType(node) != "strong" {
			return -1
		}
		label := strings.TrimSpace(PlainText(node))
		if !strings.HasSuffix(label, ":") {
			return -1
		}
		return index
	}
	return -1
}

func answerAfterStrong(values []any, strongIndex int) string {
	var builder strings.Builder
	for _, value := range values[strongIndex+1:] {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		builder.WriteString(PlainText(node))
	}
	return strings.TrimSpace(builder.String())
}

func containsQuestion(node map[string]any) bool {
	values, ok := children(node)
	if !ok {
		return false
	}
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if nodeType(child) == "paragraph" {
			inline, _ := children(child)
			if firstQuestionStrongIndex(inline) >= 0 {
				return true
			}
		}
		if containsQuestion(child) {
			return true
		}
	}
	return false
}

func nodeAtPath(document Document, path []int) (map[string]any, error) {
	current := map[string]any(document)
	for _, index := range path {
		values, ok := children(current)
		if !ok {
			return nil, fmt.Errorf("invalid question path: node %q has no children", nodeType(current))
		}
		if index < 0 || index >= len(values) {
			return nil, fmt.Errorf("invalid question path: child index %d is out of range", index)
		}
		next, ok := values[index].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid question path: child %d is not an mdast node", index)
		}
		current = next
	}
	return current, nil
}

func nodeType(node map[string]any) string {
	return stringValue(node, "type")
}

func children(node map[string]any) ([]any, bool) {
	values, ok := node["children"].([]any)
	return values, ok
}

func stringValue(node map[string]any, key string) string {
	value, _ := node[key].(string)
	return value
}

func intValue(node map[string]any, key string, fallback int) int {
	value, ok := node[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func cleanText(value string) string {
	fields := strings.Fields(strings.ReplaceAll(value, "\u00a0", " "))
	return strings.Join(fields, " ")
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func normalizeID(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func appendPath(path []int, index int) []int {
	result := make([]int, len(path)+1)
	copy(result, path)
	result[len(path)] = index
	return result
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}
