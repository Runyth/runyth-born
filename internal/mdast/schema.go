package mdast

import (
	"fmt"
	"strings"
)

// ValidateQuestionnaire enforces the questionnaire grammar. A template that
// does not match is rejected. Nothing in the grammar is optional except the
// preamble and a heading's guidance block.
//
//	document     = H1, preamble?, chapter+
//	preamble     = paragraph+
//	chapter      = H2, guidance?, (question-list | subchapter+)
//	subchapter   = H3, guidance?, (question-list | detail+)
//	detail       = H4, guidance?, question-list
//	guidance     = (paragraph | blockquote | example-list)+
//	question-list = one unordered list; every item is exactly "- **Label:**"
//	example-list  = one unordered list; no item is a question
//
// A heading owns only the guidance that follows it immediately. The next
// heading or question list closes that guidance. A deeper heading keeps
// parent guidance. A heading of the same depth replaces only its own level.
// Nothing follows a question list except the next legal heading. A heading
// may not skip a depth. The preamble is shown with the first question and is
// not repeated later.
func ValidateQuestionnaire(document Document) error {
	if err := Validate(document); err != nil {
		return err
	}
	nodes, ok := children(document)
	if !ok {
		return fmt.Errorf("questionnaire root has no children")
	}
	parser := schemaParser{nodes: nodes}
	if err := parser.document(); err != nil {
		return fmt.Errorf("questionnaire schema: %w", err)
	}
	if parser.index != len(parser.nodes) {
		return fmt.Errorf("questionnaire schema: %s", parser.unexpected("end of document"))
	}
	return nil
}

type schemaParser struct {
	nodes []any
	index int
}

func (parser *schemaParser) document() error {
	if err := parser.heading(1, "document title"); err != nil {
		return err
	}
	for parser.kind() == "paragraph" || parser.kind() == "blockquote" {
		if err := parser.guidanceNode("preamble"); err != nil {
			return err
		}
	}
	if parser.done() || parser.kind() != "heading" || parser.depth() != 2 {
		return fmt.Errorf("%s", parser.unexpected("a chapter heading of depth 2"))
	}
	for !parser.done() {
		if err := parser.chapter(); err != nil {
			return err
		}
	}
	return nil
}

func (parser *schemaParser) chapter() error {
	title, err := parser.headingTitle(2, "chapter")
	if err != nil {
		return err
	}
	if err := parser.guidance(title); err != nil {
		return err
	}
	if parser.kind() == "list" {
		if err := parser.questionList(title); err != nil {
			return err
		}
		return parser.closedQuestionList(title, 2)
	}
	if parser.depth() != 3 {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("a question list or a depth-3 subchapter under %q", title)))
	}
	for parser.depth() == 3 {
		if err := parser.subchapter(); err != nil {
			return err
		}
	}
	if !parser.done() && parser.depth() > 2 {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("the next chapter after %q", title)))
	}
	return nil
}

func (parser *schemaParser) subchapter() error {
	title, err := parser.headingTitle(3, "subchapter")
	if err != nil {
		return err
	}
	if err := parser.guidance(title); err != nil {
		return err
	}
	if parser.kind() == "list" {
		if err := parser.questionList(title); err != nil {
			return err
		}
		return parser.closedQuestionList(title, 3)
	}
	if parser.depth() != 4 {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("a question list or a depth-4 detail under %q", title)))
	}
	for parser.depth() == 4 {
		if err := parser.detail(); err != nil {
			return err
		}
	}
	if !parser.done() && parser.depth() > 3 {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("the next subchapter after %q", title)))
	}
	return nil
}

func (parser *schemaParser) detail() error {
	title, err := parser.headingTitle(4, "detail")
	if err != nil {
		return err
	}
	if err := parser.guidance(title); err != nil {
		return err
	}
	if err := parser.questionList(title); err != nil {
		return err
	}
	return parser.closedQuestionList(title, 4)
}

func (parser *schemaParser) guidance(owner string) error {
	for {
		switch parser.kind() {
		case "paragraph", "blockquote":
			if err := parser.guidanceNode(owner); err != nil {
				return err
			}
		case "list":
			if parser.questionListAt() {
				return nil
			}
			if err := parser.exampleList(owner); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func (parser *schemaParser) closedQuestionList(owner string, depth int) error {
	if parser.done() {
		return nil
	}
	if parser.kind() == "heading" && parser.depth() <= depth {
		return nil
	}
	return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("the next heading after the question list of %q", owner)))
}

func (parser *schemaParser) heading(depth int, role string) error {
	_, err := parser.headingTitle(depth, role)
	return err
}

func (parser *schemaParser) headingTitle(depth int, role string) (string, error) {
	node, ok := parser.node()
	if !ok || nodeType(node) != "heading" || intValue(node, "depth", 0) != depth {
		return "", fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("a %s heading of depth %d", role, depth)))
	}
	title := cleanText(PlainText(node))
	if title == "" {
		return "", fmt.Errorf("heading %d is empty", parser.index+1)
	}
	parser.index++
	return title, nil
}

func (parser *schemaParser) guidanceNode(owner string) error {
	if parser.kind() == "blockquote" {
		return parser.blockquote(owner)
	}
	return parser.paragraph()
}

func (parser *schemaParser) paragraph() error {
	node, _ := parser.node()
	if cleanText(PlainText(node)) == "" {
		return fmt.Errorf("paragraph %d is empty", parser.index+1)
	}
	if err := validateGuidanceInlines(node); err != nil {
		return fmt.Errorf("paragraph %d: %w", parser.index+1, err)
	}
	parser.index++
	return nil
}

func (parser *schemaParser) blockquote(owner string) error {
	node, ok := parser.node()
	if !ok || nodeType(node) != "blockquote" {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("a citation under %q", owner)))
	}
	values, ok := children(node)
	if !ok || len(values) == 0 {
		return fmt.Errorf("citation under %q is empty", owner)
	}
	for index, value := range values {
		child, ok := value.(map[string]any)
		if !ok || nodeType(child) != "paragraph" {
			return fmt.Errorf("citation under %q may contain only paragraphs", owner)
		}
		if cleanText(PlainText(child)) == "" {
			return fmt.Errorf("citation paragraph %d under %q is empty", index+1, owner)
		}
		if err := validateGuidanceInlines(child); err != nil {
			return fmt.Errorf("citation under %q: %w", owner, err)
		}
	}
	parser.index++
	return nil
}

func validateGuidanceInlines(node map[string]any) error {
	values, ok := children(node)
	if !ok {
		return nil
	}
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch nodeType(child) {
		case "text", "inlineCode", "break":
		case "strong", "emphasis":
			if err := validateGuidanceInlines(child); err != nil {
				return err
			}
		default:
			return fmt.Errorf("inline %q is not allowed in guidance", nodeType(child))
		}
	}
	return nil
}

func (parser *schemaParser) questionList(owner string) error {
	node, ok := parser.node()
	if !ok || !parser.questionListAt() {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("one question list under %q", owner)))
	}
	items, ok := children(node)
	if !ok || len(items) == 0 {
		return fmt.Errorf("question list under %q is empty", owner)
	}
	for itemIndex, item := range items {
		itemNode, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("question %d under %q is not a list item", itemIndex+1, owner)
		}
		if _, checked := itemNode["checked"].(bool); checked {
			return fmt.Errorf("question %d under %q is a task item", itemIndex+1, owner)
		}
		itemChildren, ok := children(itemNode)
		if !ok || len(itemChildren) != 1 {
			return fmt.Errorf("question %d under %q must contain one paragraph and nothing else", itemIndex+1, owner)
		}
		paragraph, ok := itemChildren[0].(map[string]any)
		if !ok || nodeType(paragraph) != "paragraph" {
			return fmt.Errorf("question %d under %q must contain one paragraph", itemIndex+1, owner)
		}
		inline, ok := children(paragraph)
		if !ok || firstQuestionStrongIndex(inline) < 0 {
			return fmt.Errorf("question %d under %q must be exactly \"- **Label:**\"", itemIndex+1, owner)
		}
		if strings.TrimSpace(answerAfterStrong(inline, firstQuestionStrongIndex(inline))) != "" {
			return fmt.Errorf("question %d under %q must not contain an answer", itemIndex+1, owner)
		}
	}
	parser.index++
	return nil
}

func (parser *schemaParser) exampleList(owner string) error {
	node, ok := parser.node()
	if !ok || nodeType(node) != "list" || orderedList(node) {
		return fmt.Errorf("%s", parser.unexpected(fmt.Sprintf("an example list under %q", owner)))
	}
	items, ok := children(node)
	if !ok || len(items) == 0 {
		return fmt.Errorf("example list under %q is empty", owner)
	}
	for itemIndex, item := range items {
		itemNode, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("example %d under %q is not a list item", itemIndex+1, owner)
		}
		if _, checked := itemNode["checked"].(bool); checked {
			return fmt.Errorf("example %d under %q is a task item", itemIndex+1, owner)
		}
		if _, _, ok := questionParagraph(itemNode, nil); ok {
			return fmt.Errorf("example %d under %q is a question; questions belong in their own list", itemIndex+1, owner)
		}
		if cleanText(PlainText(itemNode)) == "" {
			return fmt.Errorf("example %d under %q is empty", itemIndex+1, owner)
		}
	}
	parser.index++
	return nil
}

func (parser *schemaParser) questionListAt() bool {
	node, ok := parser.node()
	if !ok || nodeType(node) != "list" || orderedList(node) {
		return false
	}
	items, ok := children(node)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		itemNode, ok := item.(map[string]any)
		if !ok {
			return false
		}
		if _, _, ok := questionParagraph(itemNode, nil); !ok {
			return false
		}
	}
	return true
}

func (parser *schemaParser) node() (map[string]any, bool) {
	if parser.done() {
		return nil, false
	}
	node, ok := parser.nodes[parser.index].(map[string]any)
	return node, ok
}

func (parser *schemaParser) kind() string {
	node, ok := parser.node()
	if !ok {
		return ""
	}
	return nodeType(node)
}

func (parser *schemaParser) depth() int {
	node, ok := parser.node()
	if !ok || nodeType(node) != "heading" {
		return 0
	}
	return intValue(node, "depth", 0)
}

func (parser *schemaParser) done() bool {
	return parser.index >= len(parser.nodes)
}

func (parser *schemaParser) unexpected(wanted string) string {
	if parser.done() {
		return fmt.Sprintf("document ended; expected %s", wanted)
	}
	node, ok := parser.node()
	if !ok {
		return fmt.Sprintf("child %d is not an mdast node; expected %s", parser.index+1, wanted)
	}
	kind := nodeType(node)
	if kind == "heading" {
		return fmt.Sprintf("heading %d %q has depth %d; expected %s", parser.index+1, cleanText(PlainText(node)), intValue(node, "depth", 0), wanted)
	}
	label := cleanText(PlainText(node))
	if len(label) > 80 {
		label = label[:80]
	}
	if label == "" {
		return fmt.Sprintf("child %d is a %s; expected %s", parser.index+1, kind, wanted)
	}
	return fmt.Sprintf("child %d is a %s %q; expected %s", parser.index+1, kind, label, wanted)
}

func orderedList(node map[string]any) bool {
	ordered, _ := node["ordered"].(bool)
	return ordered
}
