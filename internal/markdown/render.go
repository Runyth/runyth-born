package markdown

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Akkitto/template-wizard/internal/mdast"
)

func Render(document mdast.Document) (string, error) {
	if err := mdast.Validate(document); err != nil {
		return "", err
	}

	renderer := &renderer{}
	output, err := renderer.renderBlocks(map[string]any(document))
	if err != nil {
		return "", err
	}
	output = strings.TrimRight(output, " \t\n") + "\n"
	return output, nil
}

type renderer struct{}

func (renderer *renderer) renderBlocks(node map[string]any) (string, error) {
	nodeType := stringValue(node, "type")
	switch nodeType {
	case "root":
		return renderer.renderBlockChildren(node, "\n\n")
	case "heading":
		depth := intValue(node, "depth", 1)
		if depth < 1 {
			depth = 1
		}
		if depth > 6 {
			depth = 6
		}
		inline, err := renderer.renderInlineChildren(node, inlineContext{})
		if err != nil {
			return "", err
		}
		return strings.Repeat("#", depth) + " " + inline, nil
	case "paragraph":
		return renderer.renderInlineChildren(node, inlineContext{})
	case "thematicBreak":
		return "---", nil
	case "blockquote":
		inner, err := renderer.renderBlockChildren(node, "\n\n")
		if err != nil {
			return "", err
		}
		return prefixLines(inner, "> ", ">"), nil
	case "list":
		return renderer.renderList(node)
	case "listItem":
		return renderer.renderListItem(node)
	case "code":
		return renderer.renderCode(node), nil
	case "html":
		return stringValue(node, "value"), nil
	case "definition":
		return renderer.renderDefinition(node), nil
	case "footnoteDefinition":
		return renderer.renderFootnoteDefinition(node)
	case "table":
		return renderer.renderTable(node)
	case "yaml":
		value := strings.TrimRight(stringValue(node, "value"), "\n")
		return "---\n" + value + "\n---", nil
	case "math":
		value := strings.TrimRight(stringValue(node, "value"), "\n")
		return "$$\n" + value + "\n$$", nil
	default:
		if _, ok := children(node); ok {
			return renderer.renderBlockChildren(node, "\n\n")
		}
		if value := stringValue(node, "value"); value != "" {
			return value, nil
		}
		return "", nil
	}
}

func (renderer *renderer) renderBlockChildren(node map[string]any, separator string) (string, error) {
	values, ok := children(node)
	if !ok {
		return "", nil
	}

	rendered := make([]string, 0, len(values))
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		block, err := renderer.renderBlocks(child)
		if err != nil {
			return "", err
		}
		if block != "" {
			rendered = append(rendered, block)
		}
	}
	return strings.Join(rendered, separator), nil
}

func (renderer *renderer) renderList(node map[string]any) (string, error) {
	values, ok := children(node)
	if !ok {
		return "", nil
	}

	ordered := boolValue(node, "ordered", false)
	start := intValue(node, "start", 1)
	spread := boolValue(node, "spread", false)
	items := make([]string, 0, len(values))

	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}

		body, err := renderer.renderListItem(item)
		if err != nil {
			return "", err
		}

		marker := "- "
		if ordered {
			marker = strconv.Itoa(start+index) + ". "
		}
		if checked, exists := optionalBool(item, "checked"); exists {
			if checked {
				marker += "[x] "
			} else {
				marker += "[ ] "
			}
		}

		items = append(items, indentListItem(body, marker))
	}

	separator := "\n"
	if spread {
		separator = "\n\n"
	}
	return strings.Join(items, separator), nil
}

func (renderer *renderer) renderListItem(node map[string]any) (string, error) {
	values, ok := children(node)
	if !ok {
		return "", nil
	}

	blocks := make([]string, 0, len(values))
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		rendered, err := renderer.renderBlocks(child)
		if err != nil {
			return "", err
		}
		if rendered != "" {
			blocks = append(blocks, rendered)
		}
	}

	separator := "\n\n"
	if !boolValue(node, "spread", false) && allSimpleBlocks(values) {
		separator = "\n"
	}
	return strings.Join(blocks, separator), nil
}

func (renderer *renderer) renderCode(node map[string]any) string {
	value := stringValue(node, "value")
	language := stringValue(node, "lang")
	meta := stringValue(node, "meta")

	fenceLength := maximumRun(value, '`') + 1
	if fenceLength < 3 {
		fenceLength = 3
	}
	fence := strings.Repeat("`", fenceLength)

	info := language
	if meta != "" {
		if info != "" {
			info += " "
		}
		info += meta
	}

	return fence + info + "\n" + strings.TrimRight(value, "\n") + "\n" + fence
}

func (renderer *renderer) renderDefinition(node map[string]any) string {
	label := stringValue(node, "label")
	if label == "" {
		label = stringValue(node, "identifier")
	}
	url := destination(stringValue(node, "url"))
	title := stringValue(node, "title")
	if title != "" {
		return "[" + escapeLabel(label) + "]: " + url + " \"" + escapeTitle(title) + "\""
	}
	return "[" + escapeLabel(label) + "]: " + url
}

func (renderer *renderer) renderFootnoteDefinition(node map[string]any) (string, error) {
	identifier := stringValue(node, "label")
	if identifier == "" {
		identifier = stringValue(node, "identifier")
	}
	body, err := renderer.renderBlockChildren(node, "\n\n")
	if err != nil {
		return "", err
	}
	return prefixFirstAndFollowing(body, "[^"+escapeLabel(identifier)+"]: ", "    "), nil
}

func (renderer *renderer) renderTable(node map[string]any) (string, error) {
	rows, ok := children(node)
	if !ok || len(rows) == 0 {
		return "", nil
	}

	table := make([][]string, 0, len(rows))
	maximumColumns := 0
	for _, rowValue := range rows {
		row, ok := rowValue.(map[string]any)
		if !ok || stringValue(row, "type") != "tableRow" {
			continue
		}
		cells, _ := children(row)
		renderedRow := make([]string, 0, len(cells))
		for _, cellValue := range cells {
			cell, ok := cellValue.(map[string]any)
			if !ok {
				continue
			}
			inline, err := renderer.renderInlineChildren(cell, inlineContext{tableCell: true})
			if err != nil {
				return "", err
			}
			renderedRow = append(renderedRow, strings.ReplaceAll(inline, "\n", "<br>"))
		}
		if len(renderedRow) > maximumColumns {
			maximumColumns = len(renderedRow)
		}
		table = append(table, renderedRow)
	}

	if len(table) == 0 || maximumColumns == 0 {
		return "", nil
	}
	for index := range table {
		for len(table[index]) < maximumColumns {
			table[index] = append(table[index], "")
		}
	}

	alignments := stringSlice(node["align"])
	separator := make([]string, maximumColumns)
	for index := 0; index < maximumColumns; index++ {
		alignment := ""
		if index < len(alignments) {
			alignment = alignments[index]
		}
		switch alignment {
		case "left":
			separator[index] = ":---"
		case "right":
			separator[index] = "---:"
		case "center":
			separator[index] = ":---:"
		default:
			separator[index] = "---"
		}
	}

	lines := make([]string, 0, len(table)+1)
	lines = append(lines, renderTableRow(table[0]))
	lines = append(lines, renderTableRow(separator))
	for _, row := range table[1:] {
		lines = append(lines, renderTableRow(row))
	}
	return strings.Join(lines, "\n"), nil
}

func (renderer *renderer) renderInlineChildren(node map[string]any, context inlineContext) (string, error) {
	values, ok := children(node)
	if !ok {
		return "", nil
	}
	var builder strings.Builder
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		rendered, err := renderer.renderInline(child, context)
		if err != nil {
			return "", err
		}
		builder.WriteString(rendered)
	}
	return builder.String(), nil
}

type inlineContext struct {
	tableCell bool
}

func (renderer *renderer) renderInline(node map[string]any, context inlineContext) (string, error) {
	switch stringValue(node, "type") {
	case "text":
		return escapeText(stringValue(node, "value"), context), nil
	case "strong":
		value, err := renderer.renderInlineChildren(node, context)
		return "**" + value + "**", err
	case "emphasis":
		value, err := renderer.renderInlineChildren(node, context)
		return "*" + value + "*", err
	case "delete":
		value, err := renderer.renderInlineChildren(node, context)
		return "~~" + value + "~~", err
	case "inlineCode":
		return renderInlineCode(stringValue(node, "value")), nil
	case "break":
		return "  \n", nil
	case "link":
		label, err := renderer.renderInlineChildren(node, context)
		if err != nil {
			return "", err
		}
		title := stringValue(node, "title")
		suffix := ""
		if title != "" {
			suffix = " \"" + escapeTitle(title) + "\""
		}
		return "[" + label + "](" + destination(stringValue(node, "url")) + suffix + ")", nil
	case "linkReference":
		label, err := renderer.renderInlineChildren(node, context)
		if err != nil {
			return "", err
		}
		reference := stringValue(node, "label")
		if reference == "" {
			reference = stringValue(node, "identifier")
		}
		return "[" + label + "][" + escapeLabel(reference) + "]", nil
	case "image":
		title := stringValue(node, "title")
		suffix := ""
		if title != "" {
			suffix = " \"" + escapeTitle(title) + "\""
		}
		return "![" + escapeLabel(stringValue(node, "alt")) + "](" + destination(stringValue(node, "url")) + suffix + ")", nil
	case "imageReference":
		reference := stringValue(node, "label")
		if reference == "" {
			reference = stringValue(node, "identifier")
		}
		return "![" + escapeLabel(stringValue(node, "alt")) + "][" + escapeLabel(reference) + "]", nil
	case "footnoteReference":
		identifier := stringValue(node, "label")
		if identifier == "" {
			identifier = stringValue(node, "identifier")
		}
		return "[^" + escapeLabel(identifier) + "]", nil
	case "html":
		return stringValue(node, "value"), nil
	case "inlineMath":
		value := stringValue(node, "value")
		delimiter := "$"
		if strings.Contains(value, "$") {
			delimiter = "$$"
		}
		return delimiter + value + delimiter, nil
	default:
		if _, ok := children(node); ok {
			return renderer.renderInlineChildren(node, context)
		}
		return escapeText(stringValue(node, "value"), context), nil
	}
}

func escapeText(value string, context inlineContext) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")

	var builder strings.Builder
	for _, character := range value {
		switch character {
		case '\\', '*', '[', ']', '<', '>':
			builder.WriteByte('\\')
			builder.WriteRune(character)
		case '_':
			// Underscores inside ordinary words do not need escaping and keeping them
			// untouched makes URLs and identifiers easier to read.
			builder.WriteRune(character)
		case '|':
			if context.tableCell {
				builder.WriteString("\\|")
			} else {
				builder.WriteRune(character)
			}
		default:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func renderInlineCode(value string) string {
	delimiterLength := maximumRun(value, '`') + 1
	delimiter := strings.Repeat("`", delimiterLength)
	padding := ""
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		padding = " "
	}
	return delimiter + padding + value + padding + delimiter
}

func destination(url string) string {
	if url == "" {
		return "<>"
	}
	if strings.ContainsAny(url, " ()<>\n\t") {
		return "<" + strings.ReplaceAll(url, ">", "%3E") + ">"
	}
	return strings.ReplaceAll(url, ")", "\\)")
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]")
	return replacer.Replace(value)
}

func escapeTitle(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"")
	return replacer.Replace(value)
}

func renderTableRow(values []string) string {
	return "| " + strings.Join(values, " | ") + " |"
}

func indentListItem(body string, marker string) string {
	if body == "" {
		return strings.TrimRight(marker, " ")
	}
	return prefixFirstAndFollowing(body, marker, strings.Repeat(" ", utf8.RuneCountInString(marker)))
}

func prefixFirstAndFollowing(value string, firstPrefix string, followingPrefix string) string {
	lines := strings.Split(value, "\n")
	var builder strings.Builder
	for index, line := range lines {
		if index > 0 {
			builder.WriteByte('\n')
			if line != "" {
				builder.WriteString(followingPrefix)
			}
		}
		if index == 0 {
			builder.WriteString(firstPrefix)
		}
		builder.WriteString(line)
	}
	return builder.String()
}

func prefixLines(value string, prefix string, blankPrefix string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if line == "" {
			lines[index] = blankPrefix
		} else {
			lines[index] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func maximumRun(value string, target rune) int {
	maximum := 0
	current := 0
	for _, character := range value {
		if character == target {
			current++
			if current > maximum {
				maximum = current
			}
		} else {
			current = 0
		}
	}
	return maximum
}

func allSimpleBlocks(values []any) bool {
	for _, value := range values {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		nodeType := stringValue(node, "type")
		if nodeType != "paragraph" && nodeType != "list" {
			return false
		}
	}
	return true
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
	if !ok || value == nil {
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

func boolValue(node map[string]any, key string, fallback bool) bool {
	value, ok := node[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func optionalBool(node map[string]any, key string) (bool, bool) {
	value, exists := node[key]
	if !exists || value == nil {
		return false, false
	}
	boolean, ok := value.(bool)
	return boolean, ok
}

func stringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item == nil {
			result = append(result, "")
			continue
		}
		result = append(result, fmt.Sprint(item))
	}
	return result
}
