package yamlmini

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Parse decodes the JSON-compatible subset of YAML commonly produced when an
// mdast JSON document is converted to YAML. It supports mappings, sequences,
// quoted and plain scalars, inline JSON arrays/objects, and | / > block scalars.
// YAML anchors, aliases, tags, and merge keys are intentionally rejected.
func Parse(data []byte) (any, error) {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	parser := &parser{lines: strings.Split(normalized, "\n")}
	parser.skipIgnorable()
	if parser.index >= len(parser.lines) {
		return nil, fmt.Errorf("YAML input is empty")
	}

	indent, err := indentation(parser.lines[parser.index])
	if err != nil {
		return nil, parser.errorAt(parser.index, err.Error())
	}

	value, err := parser.parseBlock(indent)
	if err != nil {
		return nil, err
	}

	parser.skipIgnorable()
	if parser.index < len(parser.lines) {
		return nil, parser.errorAt(parser.index, "unexpected trailing YAML content")
	}

	return value, nil
}

type parser struct {
	lines []string
	index int
}

func (p *parser) parseBlock(indent int) (any, error) {
	p.skipIgnorable()
	if p.index >= len(p.lines) {
		return nil, nil
	}

	currentIndent, err := indentation(p.lines[p.index])
	if err != nil {
		return nil, p.errorAt(p.index, err.Error())
	}
	if currentIndent < indent {
		return nil, nil
	}
	if currentIndent != indent {
		return nil, p.errorAt(p.index, fmt.Sprintf("expected indentation %d, found %d", indent, currentIndent))
	}

	content := strings.TrimSpace(p.lines[p.index])
	if isSequenceLine(content) {
		return p.parseSequence(indent)
	}
	return p.parseMapping(indent, nil)
}

func (p *parser) parseSequence(indent int) ([]any, error) {
	values := make([]any, 0)

	for {
		p.skipIgnorable()
		if p.index >= len(p.lines) {
			break
		}

		lineIndent, err := indentation(p.lines[p.index])
		if err != nil {
			return nil, p.errorAt(p.index, err.Error())
		}
		if lineIndent < indent {
			break
		}
		if lineIndent > indent {
			return nil, p.errorAt(p.index, fmt.Sprintf("unexpected indentation %d inside sequence at indentation %d", lineIndent, indent))
		}

		content := strings.TrimSpace(p.lines[p.index])
		if !isSequenceLine(content) {
			break
		}

		rest := strings.TrimSpace(strings.TrimPrefix(content, "-"))
		p.index++

		if rest == "" {
			nextIndent, ok, err := p.nextContentIndent()
			if err != nil {
				return nil, err
			}
			if !ok || nextIndent <= indent {
				values = append(values, nil)
				continue
			}
			nested, err := p.parseBlock(nextIndent)
			if err != nil {
				return nil, err
			}
			values = append(values, nested)
			continue
		}

		if key, rawValue, ok := splitMappingEntry(rest); ok {
			item := map[string]any{}
			if err := p.assignMappingValue(item, key, rawValue, indent+2); err != nil {
				return nil, err
			}

			p.skipIgnorable()
			if p.index < len(p.lines) {
				continuationIndent, err := indentation(p.lines[p.index])
				if err != nil {
					return nil, p.errorAt(p.index, err.Error())
				}
				if continuationIndent > indent {
					if continuationIndent != indent+2 {
						return nil, p.errorAt(p.index, fmt.Sprintf("expected sequence mapping continuation at indentation %d, found %d", indent+2, continuationIndent))
					}
					if _, ok := item["__yamlmini_impossible__"]; ok {
						panic("unreachable")
					}
					parsed, err := p.parseMapping(indent+2, item)
					if err != nil {
						return nil, err
					}
					item = parsed
				}
			}

			values = append(values, item)
			continue
		}

		scalar, err := parseScalar(rest)
		if err != nil {
			return nil, p.errorAt(p.index-1, err.Error())
		}
		values = append(values, scalar)
	}

	return values, nil
}

func (p *parser) parseMapping(indent int, initial map[string]any) (map[string]any, error) {
	result := initial
	if result == nil {
		result = map[string]any{}
	}

	for {
		p.skipIgnorable()
		if p.index >= len(p.lines) {
			break
		}

		lineIndent, err := indentation(p.lines[p.index])
		if err != nil {
			return nil, p.errorAt(p.index, err.Error())
		}
		if lineIndent < indent {
			break
		}
		if lineIndent > indent {
			return nil, p.errorAt(p.index, fmt.Sprintf("unexpected indentation %d inside mapping at indentation %d", lineIndent, indent))
		}

		content := strings.TrimSpace(p.lines[p.index])
		if isSequenceLine(content) {
			break
		}

		key, rawValue, ok := splitMappingEntry(content)
		if !ok {
			return nil, p.errorAt(p.index, "expected a YAML mapping entry")
		}
		if key == "<<" {
			return nil, p.errorAt(p.index, "YAML merge keys are not supported")
		}
		if _, exists := result[key]; exists {
			return nil, p.errorAt(p.index, fmt.Sprintf("duplicate mapping key %q", key))
		}

		p.index++
		if err := p.assignMappingValue(result, key, rawValue, indent+2); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (p *parser) assignMappingValue(target map[string]any, key string, rawValue string, childIndent int) error {
	rawValue = strings.TrimSpace(rawValue)

	if isBlockScalarHeader(rawValue) {
		value, err := p.parseBlockScalar(childIndent, rawValue)
		if err != nil {
			return err
		}
		target[key] = value
		return nil
	}

	if rawValue != "" {
		scalar, err := parseScalar(rawValue)
		if err != nil {
			return p.errorAt(max(0, p.index-1), err.Error())
		}
		target[key] = scalar
		return nil
	}

	nextIndent, ok, err := p.nextContentIndent()
	if err != nil {
		return err
	}
	if !ok || nextIndent < childIndent {
		target[key] = nil
		return nil
	}
	if nextIndent != childIndent {
		return p.errorAt(p.index, fmt.Sprintf("expected nested value at indentation %d, found %d", childIndent, nextIndent))
	}

	nested, err := p.parseBlock(childIndent)
	if err != nil {
		return err
	}
	target[key] = nested
	return nil
}

func (p *parser) parseBlockScalar(childIndent int, header string) (string, error) {
	style := header[0]
	chomp := byte(0)
	if len(header) > 1 {
		switch header[1] {
		case '-', '+':
			chomp = header[1]
		default:
			if !unicode.IsDigit(rune(header[1])) {
				return "", p.errorAt(max(0, p.index-1), fmt.Sprintf("unsupported block scalar header %q", header))
			}
		}
	}

	start := p.index
	minimumIndent := -1
	end := start
	for end < len(p.lines) {
		raw := p.lines[end]
		if strings.TrimSpace(raw) == "" {
			end++
			continue
		}
		lineIndent, err := indentation(raw)
		if err != nil {
			return "", p.errorAt(end, err.Error())
		}
		if lineIndent < childIndent {
			break
		}
		if minimumIndent == -1 || lineIndent < minimumIndent {
			minimumIndent = lineIndent
		}
		end++
	}

	if minimumIndent == -1 {
		p.index = end
		return "", nil
	}

	contentLines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		raw := p.lines[i]
		if strings.TrimSpace(raw) == "" {
			contentLines = append(contentLines, "")
			continue
		}
		if len(raw) < minimumIndent {
			return "", p.errorAt(i, "invalid block scalar indentation")
		}
		contentLines = append(contentLines, raw[minimumIndent:])
	}
	p.index = end

	var value string
	if style == '>' {
		value = foldLines(contentLines)
	} else {
		value = strings.Join(contentLines, "\n")
	}

	switch chomp {
	case '-':
		value = strings.TrimRight(value, "\n")
	case '+':
		value += "\n"
	default:
		value = strings.TrimRight(value, "\n") + "\n"
	}

	return value, nil
}

func foldLines(lines []string) string {
	var builder strings.Builder
	for i, line := range lines {
		if i == 0 {
			builder.WriteString(line)
			continue
		}
		previousBlank := lines[i-1] == ""
		currentBlank := line == ""
		if previousBlank || currentBlank {
			builder.WriteByte('\n')
		} else {
			builder.WriteByte(' ')
		}
		builder.WriteString(line)
	}
	return builder.String()
}

func parseScalar(raw string) (any, error) {
	value := strings.TrimSpace(stripComment(raw))
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") || strings.HasPrefix(value, "!") {
		return nil, fmt.Errorf("YAML anchors, aliases, and tags are not supported")
	}

	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err == nil {
			return decoded, nil
		}
	}

	if strings.HasPrefix(value, "\"") {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return nil, fmt.Errorf("invalid double-quoted scalar: %w", err)
		}
		return decoded, nil
	}

	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return nil, fmt.Errorf("unterminated single-quoted scalar")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}

	switch strings.ToLower(value) {
	case "null", "~":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}

	if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
		return integer, nil
	}
	if unsigned, err := strconv.ParseUint(value, 10, 64); err == nil {
		return unsigned, nil
	}
	if floating, err := strconv.ParseFloat(value, 64); err == nil && strings.ContainsAny(value, ".eE") {
		return floating, nil
	}

	return value, nil
}

func splitMappingEntry(line string) (string, string, bool) {
	quote := rune(0)
	escaped := false
	for index, character := range line {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == ':' {
			remainder := line[index+1:]
			if remainder == "" || unicode.IsSpace(firstRune(remainder)) {
				rawKey := strings.TrimSpace(line[:index])
				if rawKey == "" {
					return "", "", false
				}
				key, err := parseKey(rawKey)
				if err != nil {
					return "", "", false
				}
				return key, strings.TrimSpace(remainder), true
			}
		}
	}
	return "", "", false
}

func parseKey(raw string) (string, error) {
	if strings.HasPrefix(raw, "\"") {
		return strconv.Unquote(raw)
	}
	if strings.HasPrefix(raw, "'") {
		if len(raw) < 2 || !strings.HasSuffix(raw, "'") {
			return "", fmt.Errorf("unterminated quoted key")
		}
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), nil
	}
	return raw, nil
}

func stripComment(raw string) string {
	quote := rune(0)
	escaped := false
	runes := []rune(raw)
	for i, character := range runes {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == '#' && (i == 0 || unicode.IsSpace(runes[i-1])) {
			return string(runes[:i])
		}
	}
	return raw
}

func isSequenceLine(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

func isBlockScalarHeader(value string) bool {
	if value == "" || (value[0] != '|' && value[0] != '>') {
		return false
	}
	if len(value) == 1 {
		return true
	}
	for _, character := range value[1:] {
		if character != '+' && character != '-' && !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

func indentation(line string) (int, error) {
	count := 0
	for _, character := range line {
		switch character {
		case ' ':
			count++
		case '\t':
			return 0, fmt.Errorf("tab indentation is not supported")
		default:
			return count, nil
		}
	}
	return count, nil
}

func (p *parser) nextContentIndent() (int, bool, error) {
	p.skipIgnorable()
	if p.index >= len(p.lines) {
		return 0, false, nil
	}
	indent, err := indentation(p.lines[p.index])
	if err != nil {
		return 0, false, p.errorAt(p.index, err.Error())
	}
	return indent, true, nil
}

func (p *parser) skipIgnorable() {
	for p.index < len(p.lines) {
		trimmed := strings.TrimSpace(p.lines[p.index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "..." {
			p.index++
			continue
		}
		break
	}
}

func (p *parser) errorAt(index int, message string) error {
	return fmt.Errorf("YAML line %d: %s", index+1, message)
}

func firstRune(value string) rune {
	for _, character := range value {
		return character
	}
	return 0
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
