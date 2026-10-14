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
		// A comment-only or separator-only document is a valid document whose
		// content is null, like the reference parser (F08).
		return nil, nil
	}

	indent, err := indentation(parser.lines[parser.index])
	if err != nil {
		return nil, parser.errorAt(parser.index, err.Error())
	}

	value, err := parser.parseBlock(indent, -1)
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

func (p *parser) parseBlock(indent int, parentIndent int) (any, error) {
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
	if content == "" {
		// TrimSpace removes Unicode whitespace that the blank-line scan did
		// not count as indentation. Indexing that empty string panics; the
		// line is a validation error, including a leftover tab (F08).
		return nil, p.rejectBlankStructuralLine()
	}
	if isSequenceLine(content) {
		return p.parseSequence(indent)
	}
	if looksLikeBlockScalarHeader(content) {
		// A bare node cannot start with the block scalar indicators: the text
		// is a header to validate and a block scalar to store (F08).
		p.index++
		return p.parseBlockScalarHeaderAndValue(parentIndent, content)
	}
	if inlineJSON(content) {
		// A flow collection is a complete typed node. A colon inside it is
		// not mapping syntax, so it is classified before key splitting (F08).
		scalar, err := parseScalar(content)
		if err != nil {
			return nil, p.errorAt(p.index, err.Error())
		}
		p.index++
		return scalar, nil
	}
	if _, _, ok := splitMappingEntry(content); ok {
		// A quoted key is mapping syntax, not a standalone quoted scalar (F08).
		return p.parseMapping(indent, nil)
	}
	if content[0] == '"' || content[0] == '\'' {
		// A quoted scalar is a complete typed node; it never continues on
		// deeper lines and it can contain colons that are not mapping syntax.
		scalar, err := parseScalar(content)
		if err != nil {
			return nil, p.errorAt(p.index, err.Error())
		}
		p.index++
		return scalar, nil
	}
	// The node is a bare plain scalar; deeper lines continue it (F08).
	p.index++
	folded, _, err := p.foldPlainContinuation(parentIndent, stripComment(content))
	if err != nil {
		return nil, p.errorAt(p.index-1, err.Error())
	}
	return folded, nil
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

		if isSequenceLine(rest) {
			// A nested block sequence begins within the item entry: the outer
			// dash token is replaced with blanks so the nested sequence keeps
			// its own entrance alignment on the same line (F08).
			entry := p.lines[p.index-1]
			inner := indent + len(entry[indent:]) - len(rest)
			p.lines[p.index-1] = strings.Repeat(" ", inner) + rest
			p.index--
			nested, err := p.parseSequence(inner)
			if err != nil {
				return nil, err
			}
			values = append(values, nested)
			continue
		}

		if rest == "" || strings.HasPrefix(rest, "#") {
			nextIndent, ok, err := p.nextContentIndent()
			if !ok || nextIndent <= indent {
				values = append(values, nil)
				continue
			}
			nested, err := p.parseBlock(nextIndent, indent)
			if err != nil {
				return nil, err
			}
			values = append(values, nested)
			continue
		}

		if looksLikeBlockScalarHeader(rest) {
			return nil, p.errorAt(p.index-1, "block scalars inside sequence items are not supported")
		}

		// Supported inline JSON collections are scalar values even when they
		// contain colons: a colon inside `{...}` or `[...]` is never sequence
		// mapping syntax (F08).
		if inlineJSON(rest) {
			scalar, err := parseScalar(rest)
			if err != nil {
				return nil, p.errorAt(p.index-1, err.Error())
			}
			values = append(values, scalar)
			continue
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

		// A plain sequence item scalar may continue on deeper lines; quoted or
		// inline JSON items never do, and a separate value indicator cannot
		// appear inside a plain scalar value (F08).
		if rest[0] != '"' && rest[0] != '\'' && !inlineJSON(rest) {
			plain := strings.TrimSpace(stripComment(rest))
			if strings.Contains(plain, ": ") {
				return nil, p.errorAt(p.index-1, "mapping values are not allowed inside a plain scalar value")
			}
			typed, _, err := p.foldPlainContinuation(indent, plain)
			if err != nil {
				return nil, p.errorAt(p.index-1, err.Error())
			}
			values = append(values, typed)
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
		if inlineJSON(strings.TrimSpace(kept)) {
			// A flow collection standing alone cannot become the simple key of
			// a block mapping entry; a colon inside `{...}` or `[...]` is not
			// mapping syntax, so the scanner rejects the key (F08).
			return nil, p.errorAt(p.index, "a flow collection cannot be a simple key here")
		}

		key, rawValue, ok := splitMappingEntry(kept)
		if !ok {
			return nil, p.errorAt(p.index, "expected a YAML mapping entry")
		}
		if key == "<<" {
			return nil, p.errorAt(p.index, "YAML merge keys are not supported")
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

	if rawValue != "" && rawValue[0] == '#' {
		// A comment after the key is not a value, but the following block may
		// be: comment-only and empty values share the nested dispatch below.
		rawValue = ""
	}

	if rawValue != "" {
		if !inlineJSON(rawValue) && rawValue[0] != '"' && rawValue[0] != '\'' {
			plain := strings.TrimSpace(stripComment(rawValue))
			// A plain scalar cannot start with the block sequence or explicit
			// key indicators, so those values scan as entry tokens and the
			// scanner rejects the document (F08).
			if isSequenceLine(plain) {
				return p.errorAt(max(0, p.index-1), "sequence entries are not allowed here")
			}
			if plain == "" {
				return p.errorAt(max(0, p.index-1), "unsupported whitespace")
			}
			if plain[0] == '?' && (len(plain) == 1 || plain[1] == ' ' || plain[1] == '\t') {
				return p.errorAt(max(0, p.index-1), "mapping keys are not allowed here")
			}
			if strings.Contains(plain, ": ") {
				// A plain scalar value cannot contain a separate value
				// indicator; the reference scanner rejects the document (F08).
				return p.errorAt(max(0, p.index-1), "mapping values are not allowed inside a plain scalar value")
			}
			if strings.TrimSpace(stripComment(rawValue)) == rawValue {
				// A plain scalar value may continue on deeper lines; a quoted
				// scalar, an inline JSON collection, or a block scalar never
				// does, and the reference scanner ends a multiline plain
				// scalar at a comment (F08).
				typed, _, err := p.foldPlainContinuation(parentIndent, plain)
				if err != nil {
					return p.errorAt(max(0, p.index-1), err.Error())
				}
				target[key] = typed
				return nil
			}
		}
		scalar, err := parseScalar(rawValue)
		if err != nil {
			return p.errorAt(max(0, p.index-1), err.Error())
		}
		target[key] = scalar
		return nil
	}

	nextIndent, ok, err := p.nextContentIndent()
	if !ok {
		target[key] = nil
		return nil
	}
	content := strings.TrimSpace(p.lines[p.index])
	if content == "" {
		return p.rejectBlankStructuralLine()
	}
	if isSequenceLine(content) && nextIndent >= parentIndent {
		// A block sequence value may start at any indentation at or past the
		// mapping's own level (F08). A shallower sequence terminates the
		// mapping instead and stays for the structural checks.
		nested, err := p.parseSequence(nextIndent)
		if err != nil {
			return err
		}
		target[key] = nested
		return nil
	}
	if looksLikeBlockScalarHeader(content) {
		// A plain scalar cannot start with the block scalar indicators: the
		// text is a header to validate and a block scalar to store. The
		// header stands on its own deeper line, so the header line is
		// consumed here and the block scan starts on the body (F08).
		p.index++
		value, err := p.parseBlockScalarHeaderAndValue(parentIndent, content)
		if err != nil {
			return err
		}
		target[key] = value
		return nil
	}
	if nextIndent <= parentIndent {
		// A sibling key at the mapping's own level starts the next mapping
		// entry rather than a nested value.
		target[key] = nil
		return nil
	}
	if inlineJSON(content) {
		// A flow collection is a complete typed value. A colon inside it is
		// not mapping syntax, so it is classified before key splitting (F08).
		scalar, err := parseScalar(content)
		if err != nil {
			return p.errorAt(p.index, err.Error())
		}
		target[key] = scalar
		p.index++
		return nil
	}
	if _, _, ok := splitMappingEntry(content); ok {
		// A nested mapping may use any indentation deeper than the parent's,
		// including a quoted first key. parseMapping consumes its first entry
		// line itself (F08).
		nested, err := p.parseMapping(nextIndent, nil)
		if err != nil {
			return err
		}
		target[key] = nested
		return nil
	}
	if content[0] == '"' || content[0] == '\'' {
		// A quoted scalar value never continues onto deeper lines (F08).
		scalar, err := parseScalar(content)
		if err != nil {
			return p.errorAt(p.index, err.Error())
		}
		target[key] = scalar
		p.index++
		return nil
	}
	// The remaining deeper content is a multiline plain scalar value whose
	// first line was just consumed (F08).
	p.index++
	folded, _, err := p.foldPlainContinuation(parentIndent, content)
	if err != nil {
		return p.errorAt(p.index-1, err.Error())
	}
	target[key] = folded
	return nil
}

// foldPlainContinuation extends a plain scalar across this and later lines:
// every nonblank line more indented than minIndent continues the scalar, joined
// with a space; a run of blank lines inside the fold yields line breaks; blank
// lines before a non-continuation stay unconsumed; a comment ends the scalar,
// and the comment's own line stays consumed; a mapping-entry-shaped line can-
// not continue it (F08). The result is typed with the reference scalar rules
// and reports whether any continuation content was consumed.
func (p *parser) foldPlainContinuation(minIndent int, first string) (any, bool, error) {
	trimmedFirst := strings.TrimSpace(first)
	keptFirst := stripComment(trimmedFirst)
	value := keptFirst
	translatingComment := keptFirst != strings.TrimSpace(first)
	if translatingComment {
		// A comment after the first scalar line ends the scalar; the caller
		// already consumed that line (F08).
		typed, err := p.typeFoldedValue(value)
		return typed, false, err
	}
	consumed := false
	pendingBlanks := 0
	for {
		saved := p.index
		if p.index >= len(p.lines) {
			break
		}
		line := p.lines[p.index]
		if p.isFinalArtifactLine(p.index) {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// A blank run belongs to the fold only when a deeper content line
			// follows before the fold's break condition.
			blanks := 0
			keeps := true
			for p.index < len(p.lines) {
				line = p.lines[p.index]
				if p.isFinalArtifactLine(p.index) {
					keeps = false
					break
				}
				if strings.TrimSpace(line) == "" {
					p.index++
					blanks++
					continue
				}
				if forbiddenTab(line) {
					keeps = false
					break
				}
				if spacePrefixLength(line) <= minIndent || strings.HasPrefix(strings.TrimSpace(line), "#") {
					keeps = false
				}
				break
			}
			if !keeps || p.index >= len(p.lines) || spacePrefixLength(p.lines[p.index]) <= minIndent {
				p.index = saved
				break
			}
			pendingBlanks += blanks
			continue
		}
		if forbiddenTab(line) {
			return nil, consumed, fmt.Errorf("tab indentation is not supported")
		}
		if spacePrefixLength(line) <= minIndent {
			p.index = saved
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			p.index = saved
			break
		}
		if _, _, ok := splitMappingEntry(trimmed); ok {
			return nil, consumed, fmt.Errorf("mapping values are not allowed inside a plain multiline value")
		}
		kept := stripComment(trimmed)
		consumed = true
		p.index++
		if kept != trimmed {
			// A trailing comment ends the scalar after its own line; deeper
			// lines after it are rejected by the structural checks (F08).
			typed, err := p.typeFoldedValue(joinFoldSegment(value, kept, &pendingBlanks))
			return typed, consumed, err
		}
		value = joinFoldSegment(value, kept, &pendingBlanks)
	}
	typed, err := p.typeFoldedValue(value)
	return typed, consumed, err
}

// joinFoldSegment appends one folded scalar segment: pending blank lines
// contribute their line breaks, a plain continuation contributes a single
// space (F08).
func joinFoldSegment(value string, kept string, pendingBlanks *int) string {
	if *pendingBlanks > 0 {
		value += strings.Repeat("\n", *pendingBlanks)
		*pendingBlanks = 0
		return value + kept
	}
	if value == "" {
		return kept
	}
	return value + " " + kept
}

// typeFoldedValue types a folded multiline scalar: the reference parser
// resolves the whole folded text with the scalar rules (F08).
func (p *parser) typeFoldedValue(value string) (any, error) {
	if value == "" {
		return nil, nil
	}
	return parseScalar(value)
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
	// breakCount accumulates the line breaks of the blank lines scanned since
	// the last content line (initially the block's leading blank lines); the
	// count stays linear even for very long blank runs.
	breakCount := 0
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
			breakCount++
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
				breakCount++
			}
			cursor++
			continue
		}
		residual := line[indent:]
		if residual == "" {
			if terminated {
				breakCount++
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
			case foldable && breakCount == 0:
				out.WriteByte(' ')
			case foldable:
			default:
				out.WriteString(previousBreak)
			}
		}
		out.WriteString(strings.Repeat("\n", breakCount))
		breakCount = 0
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
		out.WriteString(strings.Repeat("\n", breakCount))
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

// inlineJSON reports whether a scalar value starts an inline JSON collection.
func inlineJSON(raw string) bool {
	value := strings.TrimLeft(raw, " \t")
	return strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")
}

// splitInlineJSON locates the end of the first top-level flow collection,
// honoring both JSON/YAML string quoting kinds and their escaping, and
// returns the collection plus everything after it, which is where a YAML
// comment may live. Quoted hashes stay inside the strings instead of acting
// as comment starts (F08).
func splitInlineJSON(raw string) (string, string, error) {
	value := strings.TrimLeft(raw, " \t")
	depth := 0
	quote := byte(0)
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				if quote == '\'' && index+1 < len(value) && value[index+1] == '\'' {
					escaped = true
					continue
				}
				quote = 0
			}
			continue
		}
		switch character {
		case '"', '\'':
			if depth > 0 {
				quote = character
			}
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return value[:index+1], value[index+1:], nil
			}
		}
	}
	return "", "", fmt.Errorf("unterminated inline JSON collection")
}

// flowScanner walks a single-line YAML flow collection with a cursor.
type flowScanner struct {
	value string
	pos   int
}

// flowNodeRead carries one read flow node: quoted reports a decoded string
// scalar, bare reports a raw scalar token that typedFlowValue finishes, and
// separator reports that the bare token stopped at a `:` value separator
// before it (F08).
type flowNodeRead struct {
	value     any
	quoted    bool
	bare      bool
	separator bool
}

// parseFlowNode reads one flow node: a nested collection, a quoted scalar, or
// a bare scalar token. The bare token ends at a separator `:` (only when
// followed by a blank or the end of the collection), at a comma or bracket, a
// comment position, or the end of input; `: ` and `- ` cannot begin one (F08).
func (s *flowScanner) parseFlowNode() (flowNodeRead, error) {
	node := flowNodeRead{}
	blanks := s.blanks()
	if s.pos >= len(s.value) {
		return node, fmt.Errorf("unterminated inline JSON collection")
	}
	if blanks && s.value[s.pos] == '#' {
		return node, fmt.Errorf("unterminated inline JSON collection")
	}
	switch s.value[s.pos] {
	case '#', '|', '>', '%', '@', '`':
		// Those indicators cannot begin a flow node: '#' opens a comment
		// that consumes the collection, the block scalar and reserved
		// indicators cannot start a plain scalar, and a '%' is a directive
		// indicator (F08).
		return node, fmt.Errorf("a flow collection entry cannot start with the indicator %c", s.value[s.pos])
	case '{':
		parsed, err := s.parseFlowMapping()
		if err != nil {
			return node, err
		}
		node.value = parsed
		return node, nil
	case '[':
		parsed, err := s.parseFlowSequence()
		if err != nil {
			return node, err
		}
		node.value = parsed
		return node, nil
	case '"':
		decoded, err := s.scanDoubleQuoted()
		if err != nil {
			return node, err
		}
		node.quoted = true
		node.value = decoded
		return node, nil
	case '\'':
		decoded, err := s.scanSingleQuoted()
		if err != nil {
			return node, err
		}
		node.quoted = true
		node.value = decoded
		return node, nil
	case ':':
		if s.pos+1 >= len(s.value) || strings.ContainsAny(s.value[s.pos+1:s.pos+2], " \t,}]") {
			return node, fmt.Errorf("empty flow scalar token")
		}
	case '-':
		if s.pos+1 < len(s.value) && (s.value[s.pos+1] == ' ' || s.value[s.pos+1] == '\t') {
			return node, fmt.Errorf("a flow collection entry cannot start with a block sequence indicator")
		}
	}

	var token strings.Builder
	for s.pos < len(s.value) {
		character := s.value[s.pos]
		if character == ',' || character == '}' || character == ']' || character == '{' || character == '[' {
			break
		}
		if character == ':' {
			if s.pos+1 >= len(s.value) {
				node.separator = true
				break
			}
			follower := rune(s.value[s.pos+1])
			if unicode.IsSpace(follower) || follower == ',' || follower == '}' || follower == ']' {
				node.separator = true
				break
			}
			token.WriteByte(character)
			s.pos++
			continue
		}
		if unicode.IsSpace(rune(character)) {
			// Blanks end the token before structure, a comment, or a colon
			// (which always separates after blanks) and are part of the token
			// when a deeper scalar word follows (F08).
			saved := s.pos
			s.blanks()
			if s.pos >= len(s.value) {
				s.pos = saved
				break
			}
			follower := s.value[s.pos]
			if follower == '#' || follower == ',' || follower == '}' || follower == ']' || follower == '{' || follower == '[' {
				s.pos = saved
				break
			}
			if follower == ':' {
				s.pos = saved
				node.separator = true
				break
			}
			token.WriteString(s.value[saved:s.pos])
			continue
		}
		token.WriteByte(character)
		s.pos++
	}
	node.bare = true
	node.value = strings.TrimSpace(token.String())
	return node, nil
}

// scanDoubleQuoted consumes a double-quoted scalar with its quotes and
// returns the decoded content (F08).
func (s *flowScanner) scanDoubleQuoted() (string, error) {
	if s.pos >= len(s.value) || s.value[s.pos] != '"' {
		return "", fmt.Errorf("not a double-quoted scalar")
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.value) {
		character := s.value[s.pos]
		if character == '\\' {
			s.pos += 2
			continue
		}
		if character == '"' {
			s.pos++
			decoded, err := strconv.Unquote(s.value[start:s.pos])
			if err != nil {
				return "", fmt.Errorf("invalid double-quoted scalar: %w", err)
			}
			return decoded, nil
		}
		s.pos++
	}
	return "", fmt.Errorf("unterminated double-quoted scalar")
}

// scanSingleQuoted consumes a single-quoted scalar with its quotes; a doubled
// quote escapes a single quote (F08).
func (s *flowScanner) scanSingleQuoted() (string, error) {
	if s.pos >= len(s.value) || s.value[s.pos] != '\'' {
		return "", fmt.Errorf("not a single-quoted scalar")
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.value) {
		if s.value[s.pos] == '\'' {
			if s.pos+1 < len(s.value) && s.value[s.pos+1] == '\'' {
				s.pos += 2
				continue
			}
			s.pos++
			return strings.ReplaceAll(s.value[start+1:s.pos-1], "''", "'"), nil
		}
		s.pos++
	}
	return "", fmt.Errorf("unterminated single-quoted scalar")
}

// flowEntryData carries one parsed flow entry: closed reports whether the
// collection's closing bracket was reached, and hasContent reports that the
// entry carried lane mapping entry or a sequence item at all (F08).
type flowEntryData struct {
	closed     bool
	hasContent bool
	key        string
	value      any
}

func (s *flowScanner) blanks() bool {
	start := s.pos
	for s.pos < len(s.value) {
		if s.value[s.pos] != ' ' && s.value[s.pos] != '\t' {
			break
		}
		s.pos++
	}
	return s.pos > start
}

func (s *flowScanner) current() byte {
	if s.pos >= len(s.value) {
		return 0
	}
	return s.value[s.pos]
}

// parseFlowDocument reads a flow collection when its boundaries were not
// located beforehand: only the location afterward matters, and a trailing
// comment is allowed (F08).
func parseFlowDocument(raw string) (any, error) {
	scanner := &flowScanner{value: raw}
	var node any
	var err error
	if scanner.current() == '{' {
		node, err = scanner.parseFlowMapping()
	} else {
		node, err = scanner.parseFlowSequence()
	}
	if err != nil {
		return nil, err
	}
	scanner.blanks()
	if scanner.pos < len(scanner.value) && scanner.value[scanner.pos] != '#' {
		return nil, fmt.Errorf("flow collection contains trailing content")
	}
	return node, nil
}

// parseFlowCollection decodes a YAML flow collection the strict JSON decoder
// rejected: bare and quoted keys and scalars interleave, trailing commas are
// allowed, and `- x`-style indicators are rejected like the reference parser
// does (F08).
func parseFlowCollection(raw string) (any, error) {
	scanner := &flowScanner{value: raw}
	if scanner.current() == '{' {
		return scanner.parseFlowMapping()
	}
	return scanner.parseFlowSequence()
}

func (s *flowScanner) parseFlowMapping() (map[string]any, error) {
	result := map[string]any{}
	s.pos++
	for {
		entry, err := s.parseFlowEntry(true)
		if err != nil {
			return nil, err
		}
		if entry.hasContent {
			// The reference constructor overwrites a repeated key with the
			// last value instead of rejecting the document (F08).
			result[entry.key] = entry.value
		}
		if entry.closed {
			return result, nil
		}
	}
}

func (s *flowScanner) parseFlowSequence() ([]any, error) {
	values := make([]any, 0)
	s.pos++
	for {
		entry, err := s.parseFlowEntry(false)
		if err != nil {
			return nil, err
		}
		if entry.hasContent {
			values = append(values, entry.value)
		}
		if entry.closed {
			return values, nil
		}
	}
}

// parseFlowEntry parses one comma-separated flow entry until an entry comma
// or the collection's closing bracket, which is consumed and reported. A
// value separator is a `:` at the entry or after a quoted entry node; bare
// scalar values are typed with the reference rules, quoted ones are decoded,
// and a nested collection cannot act as a mapping key (F08).
func (s *flowScanner) parseFlowEntry(mapping bool) (flowEntryData, error) {
	entry := flowEntryData{}
	blanks := s.blanks()
	if s.pos >= len(s.value) || blanks && s.value[s.pos] == '#' {
		return entry, fmt.Errorf("unterminated inline JSON collection")
	}
	closer := byte('}')
	if !mapping {
		closer = ']'
	}
	if s.value[s.pos] == closer {
		s.pos++
		entry.closed = true
		return entry, nil
	}
	if s.value[s.pos] == ',' {
		return entry, fmt.Errorf("empty flow collection entry")
	}

	keyNode, err := s.parseFlowNode()
	if err != nil {
		return entry, err
	}

	separator := keyNode.separator
	if separator {
		// The bare scanner stopped before the separating blanks; consume them
		// and the ':' itself (F08).
		s.blanks()
		s.pos++
	} else {
		s.blanks()
		if s.pos < len(s.value) && s.value[s.pos] == '#' {
			return entry, fmt.Errorf("unterminated inline JSON collection")
		}
		if s.current() == ':' {
			// After a quoted scalar any ':' opens the value, as in {"a":1};
			// after a nested collection it would form a complex mapping key,
			// which is not supported here (F08).
			if keyNode.quoted {
				separator = true
				s.pos++
			} else if !keyNode.bare {
				return entry, fmt.Errorf("flow collections cannot be mapping keys here")
			}
		}
	}
	if s.pos >= len(s.value) {
		return entry, fmt.Errorf("unterminated inline JSON collection")
	}

	valueNode := keyNode
	valueEmpty := false
	if separator {
		blanks = s.blanks()
		if s.pos >= len(s.value) || blanks && s.value[s.pos] == '#' {
			return entry, fmt.Errorf("unterminated inline JSON collection")
		}
		if s.value[s.pos] == closer || s.value[s.pos] == ',' {
			valueEmpty = true
		} else {
			valueNode, err = s.parseFlowNode()
			if err != nil {
				return entry, err
			}
		}
	}

	if mapping {
		keyText, ok := keyNode.value.(string)
		if !ok {
			return entry, fmt.Errorf("flow mapping keys must be scalars")
		}
		entry.hasContent = true
		entry.key = keyText
		if !separator {
			entry.value = nil
		} else if valueEmpty {
			entry.value = nil
		} else if entry.value, err = s.typedFlowValue(valueNode); err != nil {
			return entry, err
		}
	} else if separator {
		keyText, ok := keyNode.value.(string)
		if !ok {
			return entry, fmt.Errorf("flow mapping keys must be scalars")
		}
		pair := map[string]any{keyText: nil}
		if !valueEmpty {
			if pair[keyText], err = s.typedFlowValue(valueNode); err != nil {
				return entry, err
			}
		}
		entry.hasContent = true
		entry.value = pair
	} else if entry.value, err = s.typedFlowValue(keyNode); err != nil {
		return entry, err
	} else {
		entry.hasContent = true
	}

	blanks = s.blanks()
	if s.pos >= len(s.value) || blanks && s.value[s.pos] == '#' {
		return entry, fmt.Errorf("unterminated inline JSON collection")
	}
	if s.value[s.pos] == ',' {
		s.pos++
		return entry, nil
	}
	if s.value[s.pos] == closer {
		s.pos++
		entry.closed = true
		return entry, nil
	}
	return entry, fmt.Errorf("flow collection contains trailing content")
}

// typedFlowValue finishes a flow token: quoted scalars are already decoded,
// nested collections count for themselves, and a bare token is typed with the
// same rules as a block plain scalar (F08).
func (s *flowScanner) typedFlowValue(node flowNodeRead) (any, error) {
	if node.quoted {
		return node.value, nil
	}
	if node.bare {
		return parseScalar(node.value.(string))
	}
	return node.value, nil
}

func parseScalar(raw string) (any, error) {
	// Inline JSON collections locate their trailing YAML comment with the
	// JSON boundary itself, so quoted hashes stay inside the strings (F08);
	// other scalars locate comments by scalar state.
	value := ""
	remainder := ""
	if inlineJSON(raw) {
		jsonPart, rest, err := splitInlineJSON(raw)
		if err != nil {
			// Quoted or bare scalars can contain quote characters inside a
			// flow token, so the boundary scan may not locate the closing
			// bracket; the reference grammar itself then decides where the
			// collection ends (F08).
			return parseFlowDocument(raw)
		}
		value = jsonPart
		remainder = strings.TrimSpace(rest)
	} else {
		value = strings.TrimSpace(stripComment(raw))
	}
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") || strings.HasPrefix(value, "!") {
		return nil, fmt.Errorf("YAML anchors, aliases, and tags are not supported")
	}

	if inlineJSON(raw) {
		if remainder != "" && !strings.HasPrefix(remainder, "#") {
			return nil, fmt.Errorf("inline JSON value contains trailing content")
		}
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			// Documents the reference parser accepts do not have to be valid
			// JSON: fall back to the YAML flow grammar, which also covers bare
			// keys, trailing commas, and shorthand entries (F08).
			parsed, flowErr := parseFlowCollection(value)
			if flowErr != nil {
				return nil, fmt.Errorf("invalid inline JSON value: %w", flowErr)
			}
			return parsed, nil
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
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
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

// rejectBlankStructuralLine reports a line that survived the blank-line scan
// only because TrimSpace then reduced it to nothing. A tab in that leftover
// keeps the same rejection as every other structural tab (F08).
func (p *parser) rejectBlankStructuralLine() error {
	if forbiddenTab(p.lines[p.index]) {
		return p.errorAt(p.index, "tabs are not supported outside quoted scalars")
	}
	return p.errorAt(p.index, "unsupported whitespace")
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
