package yamlmini

import (
	"encoding/json"
	"fmt"
	"io"
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

	parser := &parser{
		lines:              strings.Split(normalized, "\n"),
		documentTerminated: strings.HasSuffix(normalized, "\n"),
	}
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
	// documentTerminated records whether the normalized input ended with a
	// line break, which makes the final split element the artifact of that
	// break rather than real content (F08).
	documentTerminated bool
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

		if forbiddenTab(p.lines[p.index]) {
			return nil, p.errorAt(p.index, "tabs are not supported outside quoted scalars")
		}

		content := strings.TrimSpace(p.lines[p.index])
		if !isSequenceLine(content) {
			break
		}

		rest := strings.TrimLeft(strings.TrimPrefix(p.lines[p.index][indent:], "-"), " \t")
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

		if looksLikeBlockScalarHeader(rest) {
			return nil, p.errorAt(p.index-1, "block scalars inside sequence items are not supported")
		}

		if key, rawValue, ok := splitMappingEntry(rest); ok {
			item := map[string]any{}
			if err := p.assignMappingValue(item, key, rawValue, indent+2, indent+2); err != nil {
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

		kept := p.lines[p.index]
		if forbiddenTab(kept) {
			return nil, p.errorAt(p.index, "tabs are not supported outside quoted scalars")
		}
		if isSequenceLine(strings.TrimSpace(kept)) {
			break
		}

		key, rawValue, ok := splitMappingEntry(kept)
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
		if err := p.assignMappingValue(result, key, rawValue, indent+2, indent); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (p *parser) assignMappingValue(target map[string]any, key string, rawValue string, childIndent int, parentIndent int) error {
	if key == "<<" {
		return p.errorAt(max(0, p.index-1), "YAML merge keys are not supported")
	}
	// Leading whitespace is trimmed, but the trailing side keeps its
	// characters: a trailing tab after a block scalar header must reach the
	// header validation (F08).
	rawValue = strings.TrimLeft(rawValue, " \t")

	// The unscaled header detection runs on the left-trimmed value; a comment
	// without a separating space is not a header.
	if looksLikeBlockScalarHeader(rawValue) {
		value, err := p.parseBlockScalarHeaderAndValue(parentIndent, rawValue)
		if err != nil {
			return err
		}
		target[key] = value
		return nil
	}

	rawValue = strings.TrimSpace(rawValue)

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

func (p *parser) parseBlockScalarHeaderAndValue(parentIndent int, rawValue string) (string, error) {
	header, rest, separatedByTab := splitHeaderToken(rawValue)
	if separatedByTab {
		// The reference scanner allows only a space or a line break after the
		// header token and its indicators; a tab there is invalid (F08).
		return "", p.errorAt(max(0, p.index-1), fmt.Sprintf("invalid block scalar header %q", rawValue))
	}
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", p.errorAt(max(0, p.index-1), fmt.Sprintf("unsupported block scalar header %q", rawValue))
	}
	chomp, explicitIndent, err := parseHeaderIndicators(header)
	if err != nil {
		return "", p.errorAt(max(0, p.index-1), err.Error())
	}
	return p.parseBlockScalar(parentIndent, header[0], chomp, explicitIndent)
}

// splitHeaderToken splits the leading [|>] header token from a trailing
// comment. A comment must be separated from the token by whitespace; the
// separator is reported so a tab can be rejected (F08).
func splitHeaderToken(rawValue string) (string, string, bool) {
	value := rawValue
	tokenEnd := len(value)
	separatorTab := false
	for index, character := range value {
		if character == ' ' || character == '\t' {
			tokenEnd = index
			separatorTab = character == '\t'
			break
		}
	}
	token := value[:tokenEnd]
	rest := strings.TrimSpace(value[tokenEnd:])
	return token, rest, separatorTab
}

// parseHeaderIndicators validates the optional chomping and indentation
// indicators, in either order, exactly once each. Indentation indicators are
// restricted to the ASCII digits 1–9 the specification defines; Unicode digit
// letters are rejected instead of accepted with altered content (F08).
func parseHeaderIndicators(header string) (byte, int, error) {
	chomp := byte(0)
	explicitIndent := 0
	sawIndentIndicator := false
	skippedStyle := false
	for _, character := range header {
		if !skippedStyle {
			// The first character is the style indicator itself; every other
			// character must be an indicator, in either order, once (F08).
			skippedStyle = true
			continue
		}
		switch {
		case character == '-' || character == '+':
			if chomp != 0 {
				return 0, 0, fmt.Errorf("duplicate chomping indicator in block scalar header %q", header)
			}
			chomp = byte(character)
		case character >= '1' && character <= '9':
			if sawIndentIndicator {
				return 0, 0, fmt.Errorf("duplicate indentation indicator in block scalar header %q", header)
			}
			sawIndentIndicator = true
			explicitIndent = int(character - '0')
		default:
			return 0, 0, fmt.Errorf("unsupported block scalar header %q", header)
		}
	}
	if sawIndentIndicator && explicitIndent == 0 {
		return 0, 0, fmt.Errorf("unsupported block scalar header %q", header)
	}
	return chomp, explicitIndent, nil
}

func looksLikeBlockScalarHeader(value string) bool {
	// A plain scalar cannot start with a block scalar indicator, so any value
	// beginning with | or > is a header to validate — or an error to report —
	// never silently-accepted literal text (F08).
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && (trimmed[0] == '|' || trimmed[0] == '>')
}

// parseBlockScalar scans a literal or folded block scalar with the exact
// semantics of the YAML 1.2 block-scalar specification as implemented by
// PyYAML (F08): indentation detection from the deepest leading blank line and
// the first content line (or the explicit indicator), blank lines below the
// scan depth contribute their line breaks while residual spaces beyond the
// indentation are content, folding keeps breaks literal around more-indented
// lines and a non-blank neighbor with no blank in between folds to a space,
// and chomping keeps the actual stored records of which content lines were
// terminated by a line break — an unterminated final line stores no trailing
// break for clip or keep.
func (p *parser) parseBlockScalar(parentIndent int, style byte, chomp byte, explicitIndent int) (string, error) {
	minIndent := parentIndent + 1
	if minIndent < 1 {
		minIndent = 1
	}

	indent := 0
	if explicitIndent > 0 {
		// The explicit indicator fixes the scan depth for the whole block.
		indent = minIndent + explicitIndent - 1
	} else {
		// Auto-detection: deepest column reached while skipping the leading
		// blank lines and the first content line's own indentation.
		maxSpaces := 0
		for scan := p.index; scan < len(p.lines); {
			line := p.lines[scan]
			if p.isFinalArtifactLine(scan) {
				break
			}
			spaces := spacePrefixLength(line)
			if spaces > maxSpaces {
				maxSpaces = spaces
			}
			if spaces < len(line) {
				break
			}
			scan++
		}
		indent = max(minIndent, maxSpaces)
	}

	artifact := func(index int) bool { return p.isFinalArtifactLine(index) }
	start := p.index
	cursor := start
	// breaks holds the line breaks of the blank lines scanned since the last
	// content line (initially the block's leading blank lines).
	breaks := ""
	for cursor < len(p.lines) {
		line := p.lines[cursor]
		if artifact(cursor) {
			break
		}
		spaces := spacePrefixLength(line)
		if spaces > indent && explicitIndent > 0 {
			// Spaces beyond a fixed scan depth are real content, not a blank
			// line (F08, significant spaces).
			break
		}
		if spaces < len(line) {
			break
		}
		if cursor+1 < len(p.lines) {
			breaks += "\n"
		}
		cursor++
	}
	p.index = cursor

	var out strings.Builder
	sawContent := false
	previousLeadSpace := false
	previousBreak := ""

	for cursor < len(p.lines) {
		line := p.lines[cursor]
		if artifact(cursor) {
			break
		}
		spaces := spacePrefixLength(line)
		terminated := cursor+1 < len(p.lines)
		if spaces < indent {
			if spaces < len(line) {
				// A shallow line with real content ends the block; the
				// surrounding mapping reports the misplaced line.
				break
			}
			// A shallow all-space line is blank: it contributes its break when
			// it is terminated and nothing at an unterminated end of input.
			if terminated {
				breaks += "\n"
			}
			cursor++
			continue
		}
		residual := line[indent:]
		if residual == "" {
			if terminated {
				breaks += "\n"
			}
			cursor++
			continue
		}

		// Fold the break of the previous content line if both neighbors are
		// non-blank and not more-indented and no blank line intervened.
		if sawContent {
			foldable := style == '>' && previousBreak == "\n" &&
				!previousLeadSpace && residual[0] != ' ' && residual[0] != '\t'
			switch {
			case foldable && breaks == "":
				out.WriteByte(' ')
			case foldable:
			default:
				out.WriteString(previousBreak)
			}
		}
		out.WriteString(breaks)
		breaks = ""
		out.WriteString(residual)
		sawContent = true
		previousLeadSpace = residual[0] == ' ' || residual[0] == '\t'
		previousBreak = ""
		if terminated {
			previousBreak = "\n"
		}
		cursor++
	}
	p.index = cursor

	// Chomping uses the physically observed line breaks: clip repeats the last
	// content line's break once (if it had one), keep replays the trailing
	// blank-line breaks too, and strip drops both (F08).
	if chomp != '-' {
		out.WriteString(previousBreak)
	}
	if chomp == '+' {
		out.WriteString(breaks)
	}
	return out.String(), nil
}

// spacePrefixLength counts the leading spaces of a line. Tabs stop the scan
// and are treated as content, matching the reference scanner.
func spacePrefixLength(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func (p *parser) isFinalArtifactLine(index int) bool {
	// The last split element is the artifact of the document's terminating
	// newline rather than a real line only when the input ended with a break;
	// otherwise it is physical content (F08).
	return p.documentTerminated && index == len(p.lines)-1
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
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("invalid inline JSON value: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("inline JSON value contains trailing content")
		}
		return decoded, nil
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
	seenContent := false
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
				if character == '\'' && index+1 < len(line) && line[index+1] == '\'' {
					escaped = true
					continue
				}
				quote = 0
			}
			continue
		}
		if !seenContent {
			if unicode.IsSpace(firstRune(line[index:])) {
				continue
			}
			seenContent = true
			if character == '\'' || character == '"' {
				quote = character
				continue
			}
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
				// Only the leading whitespace is trimmed: a trailing tab after
				// the value is invalid where the block scalar header expects a
				// comment or line break, and must stay visible to the header
				// parser (F08).
				return key, strings.TrimLeft(remainder, " \t"), true
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

// stripComment removes a trailing YAML comment. Quoted state starts only at
// the first content character of the scalar, so plain scalars containing
// apostrophes or quotes are not misread, and YAML doubled single quotes stay
// inside the scalar (F08).
func stripComment(raw string) string {
	quote := rune(0)
	escaped := false
	seenContent := false
	runes := []rune(raw)
	for i := 0; i < len(runes); i++ {
		character := runes[i]
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
				if character == '\'' && i+1 < len(runes) && runes[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if !seenContent {
			if unicode.IsSpace(character) {
				continue
			}
			seenContent = true
			if character == '\'' || character == '"' {
				quote = character
				continue
			}
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
		line := p.lines[p.index]
		if forbiddenTab(line) {
			// A tab outside a quoted scalar or comment cannot begin a token;
			// stop so the caller's structural check rejects the document (F08).
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "..." {
			p.index++
			continue
		}
		break
	}
}

// forbiddenTab reports whether a structural line contains a tab outside a
// quoted scalar or a comment. The reference scanner rejects tabs in every
// other whitespace position, so block scalar content lines are handled by
// their own scanner and never see this check (F08).
func forbiddenTab(line string) bool {
	quote := byte(0)
	escaped := false
	precedingSpace := true
	for index := 0; index < len(line); index++ {
		character := line[index]
		if quote == '"' {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if character == '\'' {
				if index+1 < len(line) && line[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		switch character {
		case '\t':
			return true
		case ' ':
			precedingSpace = true
		case '#':
			if precedingSpace {
				// The rest of the line is a comment; its characters are free.
				return false
			}
			precedingSpace = false
		case '"', '\'':
			if precedingSpace {
				quote = character
			}
			precedingSpace = false
		default:
			precedingSpace = false
		}
	}
	return false
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
