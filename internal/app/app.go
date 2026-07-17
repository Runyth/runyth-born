package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Akkitto/template-wizard/internal/console"
	"github.com/Akkitto/template-wizard/internal/markdown"
	"github.com/Akkitto/template-wizard/internal/mdast"
	"github.com/Akkitto/template-wizard/internal/prompt"
)

type options struct {
	templatePath string
	outputPath   string
	directory    string
	restart      bool
	list         bool
	showVersion  bool
	showAIPrompt bool
}

type savedState struct {
	Version             int              `json:"version"`
	TemplatePath        string           `json:"template_path"`
	TemplateFingerprint string           `json:"template_fingerprint"`
	OutputPath          string           `json:"output_path"`
	Answers             []mdast.Question `json:"answers"`
}

func Run(arguments []string, input io.Reader, output io.Writer, errorOutput io.Writer, version string) error {
	parsed, err := parseOptions(arguments, errorOutput)
	if err != nil {
		return err
	}
	if parsed.showVersion {
		fmt.Fprintln(output, version)
		return nil
	}
	if parsed.showAIPrompt {
		fmt.Fprint(output, prompt.TemplateAuthoring())
		return nil
	}

	candidates, err := mdast.Discover(parsed.directory)
	if err != nil {
		return err
	}
	if parsed.list {
		if len(candidates) == 0 {
			fmt.Fprintln(output, "No valid mdast JSON or YAML templates found.")
			return nil
		}
		for _, candidate := range candidates {
			fmt.Fprintln(output, candidate)
		}
		return nil
	}

	reader := bufio.NewReader(input)
	templatePath, err := resolveTemplatePath(parsed.templatePath, parsed.directory, candidates, reader, output)
	if err != nil {
		return err
	}

	document, err := mdast.LoadFile(templatePath)
	if err != nil {
		return err
	}

	outputPath := parsed.outputPath
	if outputPath == "" {
		outputPath = defaultOutputPath(templatePath)
	}
	if !filepath.IsAbs(outputPath) {
		absolute, err := filepath.Abs(outputPath)
		if err == nil {
			outputPath = absolute
		}
	}

	statePath := outputPath + ".answers.json"
	if !parsed.restart {
		state, err := loadState(statePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load saved answers: %w", err)
		}
		if state != nil {
			if _, err := mdast.MergeAnswers(document, state.Answers); err != nil {
				return fmt.Errorf("merge saved answers into current template: %w", err)
			}
		}
	}

	questions, err := mdast.ExtractQuestions(document)
	if err != nil {
		return err
	}
	if len(questions) == 0 {
		return fmt.Errorf("template contains no recognised questions; use list items that begin with a bold label ending in a colon, for example: - **Name:**")
	}

	consoleReader := console.New(input, reader, output)
	defer func() {
		_ = consoleReader.Close()
	}()

	session := &wizardSession{
		document:     document,
		templatePath: templatePath,
		outputPath:   outputPath,
		statePath:    statePath,
		input:        consoleReader,
		output:       output,
	}

	if err := session.save(); err != nil {
		return err
	}
	return session.run()
}

func parseOptions(arguments []string, errorOutput io.Writer) (options, error) {
	parsed := options{}
	flags := flag.NewFlagSet("template-wizard", flag.ContinueOnError)
	flags.SetOutput(errorOutput)

	flags.StringVar(&parsed.templatePath, "template", "", "path to an mdast JSON or YAML template")
	flags.StringVar(&parsed.templatePath, "t", "", "path to an mdast JSON or YAML template")
	flags.StringVar(&parsed.outputPath, "output", "", "path to the filled Markdown output file")
	flags.StringVar(&parsed.outputPath, "o", "", "path to the filled Markdown output file")
	flags.StringVar(&parsed.directory, "directory", ".", "directory to search for templates")
	flags.StringVar(&parsed.directory, "d", ".", "directory to search for templates")
	flags.BoolVar(&parsed.restart, "restart", false, "ignore previously saved answers and start from the current template")
	flags.BoolVar(&parsed.list, "list", false, "list valid templates in the search directory and exit")
	flags.BoolVar(&parsed.showVersion, "version", false, "print the version and exit")
	flags.BoolVar(&parsed.showAIPrompt, "ai-template-prompt", false, "print the bundled AI prompt for authoring compatible Markdown templates and exit")

	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: template-wizard [options]")
		fmt.Fprintln(errorOutput)
		fmt.Fprintln(errorOutput, "Without --template, the program searches the local directory for valid mdast .json, .yaml, or .yml files.")
		fmt.Fprintln(errorOutput)
		flags.PrintDefaults()
	}

	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	absoluteDirectory, err := filepath.Abs(parsed.directory)
	if err != nil {
		return options{}, fmt.Errorf("resolve search directory: %w", err)
	}
	parsed.directory = absoluteDirectory
	return parsed, nil
}

func resolveTemplatePath(explicit string, directory string, candidates []string, input *bufio.Reader, output io.Writer) (string, error) {
	if explicit != "" {
		path := explicit
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve template path: %w", err)
		}
		if _, err := mdast.LoadFile(absolute); err != nil {
			return "", err
		}
		return absolute, nil
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no valid mdast JSON or YAML template found in %s", directory)
	case 1:
		return candidates[0], nil
	}

	fmt.Fprintln(output, "Several template files were found:")
	for index, candidate := range candidates {
		fmt.Fprintf(output, "  %d. %s\n", index+1, filepath.Base(candidate))
	}
	fmt.Fprintln(output)

	for {
		fmt.Fprintf(output, "Choose a template [1-%d]: ", len(candidates))
		line, err := readLine(input)
		if err != nil {
			return "", err
		}
		selection, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && selection >= 1 && selection <= len(candidates) {
			return candidates[selection-1], nil
		}
		fmt.Fprintln(output, "Please enter one of the displayed numbers.")
	}
}

func defaultOutputPath(templatePath string) string {
	directory := filepath.Dir(templatePath)
	base := filepath.Base(templatePath)
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	for _, suffix := range []string{".mdast", "-mdast", "_mdast"} {
		stem = strings.TrimSuffix(stem, suffix)
	}
	return filepath.Join(directory, stem+"-filled.md")
}

func loadState(path string) (*savedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state savedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if state.Version != 1 {
		return nil, fmt.Errorf("unsupported answer-state version %d", state.Version)
	}
	return &state, nil
}

func writeState(path string, state savedState) error {
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode answer state: %w", err)
	}
	encoded = append(encoded, '\n')
	return atomicWrite(path, encoded, 0o600)
}

func atomicWrite(path string, data []byte, permissions os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create output directory %q: %w", directory, err)
	}

	temporary, err := os.CreateTemp(directory, ".template-wizard-*")
	if err != nil {
		return fmt.Errorf("create temporary output file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(permissions); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary output permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary output file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush temporary output file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output file: %w", err)
	}

	// os.Rename replaces an existing file atomically on Unix. Windows does not
	// replace an existing destination, so remove it first there. The generated
	// Markdown is also backed by the answer-state file and can be reconstructed.
	if err := os.Rename(temporaryPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace output file %q: %w", path, err)
		}
		if retryErr := os.Rename(temporaryPath, path); retryErr != nil {
			return fmt.Errorf("replace output file %q: %w", path, retryErr)
		}
	}

	keepTemporary = false
	return nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

type wizardSession struct {
	document     mdast.Document
	templatePath string
	outputPath   string
	statePath    string
	input        *console.Reader
	output       io.Writer
}

func (session *wizardSession) run() error {
	questions, err := mdast.ExtractQuestions(session.document)
	if err != nil {
		return err
	}

	current := firstUnanswered(questions)
	if current >= len(questions) {
		current = 0
	}

	session.printWelcome(len(questions))

	for current >= 0 && current < len(questions) {
		questions, err = mdast.ExtractQuestions(session.document)
		if err != nil {
			return err
		}
		question := questions[current]
		session.printQuestion(current, questions)

		result, err := session.input.ReadPrompt()
		if errors.Is(err, console.ErrInterrupted) {
			quit, confirmErr := session.confirmQuit()
			if confirmErr != nil {
				return confirmErr
			}
			if quit {
				return nil
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(session.output)
			fmt.Fprintln(session.output, "Input ended. All committed answers are already saved.")
			return nil
		}
		if err != nil {
			return err
		}

		switch result.Navigation {
		case console.NavigationPrevious:
			if current > 0 {
				current--
			} else {
				fmt.Fprintln(session.output, "Already at the first question.")
			}
			continue
		case console.NavigationNext:
			if current+1 >= len(questions) {
				fmt.Fprintln(session.output, "Already at the last question.")
			} else {
				current++
			}
			continue
		}

		command, argument, isCommand := parseCommand(result.Line)
		if isCommand {
			switch command {
			case "back", "previous", "prev", "p":
				if current > 0 {
					current--
				} else {
					fmt.Fprintln(session.output, "Already at the first question.")
				}
			case "next", "n", "skip":
				if current+1 >= len(questions) {
					return session.finish(questions)
				}
				current++
			case "clear":
				if err := mdast.SetAnswer(session.document, question, ""); err != nil {
					return err
				}
				if err := session.save(); err != nil {
					return err
				}
				fmt.Fprintln(session.output, "Answer cleared and saved.")
			case "multi", "multiline":
				answer, err := session.readMultiline()
				if err != nil {
					return err
				}
				if err := mdast.SetAnswer(session.document, question, answer); err != nil {
					return err
				}
				if err := session.save(); err != nil {
					return err
				}
				if current+1 >= len(questions) {
					questions, _ = mdast.ExtractQuestions(session.document)
					return session.finish(questions)
				}
				current++
			case "goto":
				target, err := strconv.Atoi(argument)
				if err != nil || target < 1 || target > len(questions) {
					fmt.Fprintf(session.output, "Use /goto with a question number from 1 to %d.\n", len(questions))
				} else {
					current = target - 1
				}
			case "g", "section":
				sections := buildSections(questions)
				target, ok := resolveSection(argument, sections)
				if !ok {
					fmt.Fprintf(session.output, "Use /g with a section number shown by /t, from 1 to %d.\n", len(sections))
				} else {
					current = sections[target].StartQuestion
				}
			case "toc", "t":
				session.printTableOfContents(questions, current)
			case "unanswered", "u":
				target := nextUnanswered(questions, current+1)
				if target < 0 {
					fmt.Fprintln(session.output, "Every question currently has an answer.")
				} else {
					current = target
				}
			case "write", "w", "finish", "done":
				return session.finish(questions)
			case "quit", "q", "exit":
				quit, err := session.confirmQuit()
				if err != nil {
					return err
				}
				if quit {
					return nil
				}
			case "help", "h", "?":
				session.printHelp()
			default:
				fmt.Fprintf(session.output, "Unknown command /%s. Type /help for available commands.\n", command)
			}
			continue
		}

		answer := strings.TrimSpace(result.Line)
		if answer == "" {
			if current+1 >= len(questions) {
				return session.finish(questions)
			}
			current++
			continue
		}

		if err := mdast.SetAnswer(session.document, question, answer); err != nil {
			return err
		}
		if err := session.save(); err != nil {
			return err
		}
		if current+1 >= len(questions) {
			questions, _ = mdast.ExtractQuestions(session.document)
			return session.finish(questions)
		}
		current++
	}

	return nil
}

func (session *wizardSession) save() error {
	markdownOutput, err := markdown.Render(session.document)
	if err != nil {
		return fmt.Errorf("render filled Markdown: %w", err)
	}
	if err := atomicWrite(session.outputPath, []byte(markdownOutput), 0o644); err != nil {
		return err
	}

	questions, err := mdast.ExtractQuestions(session.document)
	if err != nil {
		return err
	}
	fingerprint, err := mdast.Fingerprint(session.document)
	if err != nil {
		return err
	}
	state := savedState{
		Version:             1,
		TemplatePath:        session.templatePath,
		TemplateFingerprint: fingerprint,
		OutputPath:          session.outputPath,
		Answers:             questions,
	}
	if err := writeState(session.statePath, state); err != nil {
		return err
	}
	return nil
}

func (session *wizardSession) printWelcome(questionCount int) {
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Template Wizard")
	fmt.Fprintln(session.output, "===============")
	fmt.Fprintf(session.output, "Template: %s\n", filepath.Base(session.templatePath))
	fmt.Fprintf(session.output, "Questions: %d\n", questionCount)
	fmt.Fprintf(session.output, "Output: %s\n", session.outputPath)
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Type an answer and press Enter. The Markdown file is updated immediately after every committed answer.")
	fmt.Fprintln(session.output, "Use Left/Up and Right/Down arrows, or /p and /n, to move between questions.")
	fmt.Fprintln(session.output, "Press Enter on an empty line to keep the current answer and continue. Type /help for commands.")
}

func (session *wizardSession) printQuestion(index int, questions []mdast.Question) {
	question := questions[index]
	answered := countAnswered(questions)

	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "------------------------------------------------------------")
	fmt.Fprintf(session.output, "Question %d of %d | Answered %d | Remaining %d\n", index+1, len(questions), answered, len(questions)-answered)
	if len(question.Breadcrumb) > 0 {
		fmt.Fprintf(session.output, "Section: %s\n", strings.Join(question.Breadcrumb, " > "))
	}
	if len(question.Context) > 0 {
		fmt.Fprintln(session.output)
		fmt.Fprintln(session.output, "Guidance:")
		for _, item := range trimContext(question.Context) {
			fmt.Fprintf(session.output, "  %s\n", item)
		}
	}
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "============================================================")
	fmt.Fprintln(session.output, "YOUR QUESTION")
	fmt.Fprintln(session.output, "------------------------------------------------------------")
	fmt.Fprintf(session.output, "%s:\n", question.Label)
	fmt.Fprintln(session.output, "============================================================")
	if question.Answer != "" {
		fmt.Fprintf(session.output, "Current answer: %s\n", strings.ReplaceAll(question.Answer, "\n", " / "))
	}
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Hint: type /help for information on commands.")
	fmt.Fprint(session.output, "> ")
}

func (session *wizardSession) readMultiline() (string, error) {
	fmt.Fprintln(session.output, "Enter the answer over multiple lines. Enter a single dot (.) on its own line to save it.")
	lines := make([]string, 0)
	for {
		fmt.Fprint(session.output, "| ")
		line, err := session.input.ReadLine()
		if err != nil {
			return "", err
		}
		if line == "." {
			return strings.TrimSpace(strings.Join(lines, "\n")), nil
		}
		lines = append(lines, line)
	}
}

func (session *wizardSession) printHelp() {
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Commands:")
	fmt.Fprintln(session.output, "  Left/Up arrow      Go to the previous question.")
	fmt.Fprintln(session.output, "  Right/Down arrow   Go to the next question.")
	fmt.Fprintln(session.output, "  /p                 Go to the previous question.")
	fmt.Fprintln(session.output, "  /n                 Keep the current answer and continue.")
	fmt.Fprintln(session.output, "  /t                 Show the table of contents for all question sections.")
	fmt.Fprintln(session.output, "  /g NUMBER          Go to a section shown by /t.")
	fmt.Fprintln(session.output, "  /goto NUMBER       Go to an exact question number.")
	fmt.Fprintln(session.output, "  /w                 Write the complete Markdown file and finish.")
	fmt.Fprintln(session.output, "  /q                 Quit after confirmation without another write.")
	fmt.Fprintln(session.output, "  /clear             Clear the current answer and save immediately.")
	fmt.Fprintln(session.output, "  /multi             Enter a multi-line answer; finish with a single dot.")
	fmt.Fprintln(session.output, "  /unanswered        Jump to the next unanswered question.")
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Long command names such as /back, /next, /finish, and /quit remain supported.")
}

func (session *wizardSession) confirmQuit() (bool, error) {
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Previously committed answers have already been autosaved.")
	fmt.Fprint(session.output, "Quit now without performing another write? [y/N]: ")
	line, err := session.input.ReadLine()
	if errors.Is(err, console.ErrInterrupted) {
		fmt.Fprintln(session.output, "Quit confirmed. No additional write was performed.")
		return true, nil
	}
	if err != nil {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		fmt.Fprintln(session.output, "Quit confirmed. No additional write was performed.")
		return true, nil
	default:
		fmt.Fprintln(session.output, "Quit cancelled.")
		return false, nil
	}
}

func (session *wizardSession) finish(questions []mdast.Question) error {
	if err := session.save(); err != nil {
		return err
	}
	answered := countAnswered(questions)
	unanswered := len(questions) - answered

	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Finished")
	fmt.Fprintln(session.output, "========")
	fmt.Fprintf(session.output, "Answered: %d of %d\n", answered, len(questions))
	fmt.Fprintf(session.output, "Unanswered: %d\n", unanswered)
	fmt.Fprintf(session.output, "Markdown file to send to the DM: %s\n", session.outputPath)
	if unanswered > 0 {
		fmt.Fprintln(session.output, "You can run the program again later to continue; existing answers will be restored.")
	}
	return nil
}

type sectionEntry struct {
	Title         string
	StartQuestion int
	EndQuestion   int
	Answered      int
	Subsections   []string
}

func buildSections(questions []mdast.Question) []sectionEntry {
	if len(questions) == 0 {
		return nil
	}

	commonDepth := commonBreadcrumbDepth(questions)
	sections := make([]sectionEntry, 0)

	for questionIndex, question := range questions {
		title, subsections := sectionParts(question.Breadcrumb, commonDepth)
		if title == "" {
			title = "Questions"
		}

		if len(sections) == 0 || sections[len(sections)-1].Title != title {
			sections = append(sections, sectionEntry{
				Title:         title,
				StartQuestion: questionIndex,
				EndQuestion:   questionIndex,
			})
		}

		section := &sections[len(sections)-1]
		section.EndQuestion = questionIndex
		if strings.TrimSpace(question.Answer) != "" {
			section.Answered++
		}
		for _, subsection := range subsections {
			if subsection != "" && !containsString(section.Subsections, subsection) {
				section.Subsections = append(section.Subsections, subsection)
			}
		}
	}

	return sections
}

func commonBreadcrumbDepth(questions []mdast.Question) int {
	if len(questions) == 0 || len(questions[0].Breadcrumb) == 0 {
		return 0
	}

	depth := len(questions[0].Breadcrumb)
	for _, question := range questions[1:] {
		if len(question.Breadcrumb) < depth {
			depth = len(question.Breadcrumb)
		}
		for index := 0; index < depth; index++ {
			if question.Breadcrumb[index] != questions[0].Breadcrumb[index] {
				depth = index
				break
			}
		}
	}
	return depth
}

func sectionParts(breadcrumb []string, commonDepth int) (string, []string) {
	if len(breadcrumb) == 0 {
		return "Questions", nil
	}
	if commonDepth < len(breadcrumb) {
		return breadcrumb[commonDepth], breadcrumb[commonDepth+1:]
	}
	return breadcrumb[len(breadcrumb)-1], nil
}

func resolveSection(argument string, sections []sectionEntry) (int, bool) {
	number, err := strconv.Atoi(strings.TrimSpace(argument))
	if err != nil || number < 1 {
		return 0, false
	}

	for index, section := range sections {
		if explicitSectionNumber(section.Title) == number {
			return index, true
		}
	}
	if number <= len(sections) {
		return number - 1, true
	}
	return 0, false
}

func explicitSectionNumber(title string) int {
	trimmed := strings.TrimSpace(title)
	end := 0
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	number, err := strconv.Atoi(trimmed[:end])
	if err != nil {
		return 0
	}
	return number
}

func (session *wizardSession) printTableOfContents(questions []mdast.Question, currentQuestion int) {
	sections := buildSections(questions)
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Table of Contents")
	fmt.Fprintln(session.output, "=================")
	for index, section := range sections {
		total := section.EndQuestion - section.StartQuestion + 1
		marker := " "
		if currentQuestion >= section.StartQuestion && currentQuestion <= section.EndQuestion {
			marker = ">"
		}
		fmt.Fprintf(
			session.output,
			"%s [%d] %s — questions %d-%d, answered %d/%d\n",
			marker,
			index+1,
			section.Title,
			section.StartQuestion+1,
			section.EndQuestion+1,
			section.Answered,
			total,
		)
		for _, subsection := range section.Subsections {
			fmt.Fprintf(session.output, "      - %s\n", subsection)
		}
	}
	fmt.Fprintln(session.output)
	fmt.Fprintln(session.output, "Use /g NUMBER to jump to a section.")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseCommand(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	fields := strings.Fields(strings.TrimPrefix(trimmed, "/"))
	if len(fields) == 0 {
		return "", "", false
	}
	command := strings.ToLower(fields[0])
	argument := ""
	if len(fields) > 1 {
		argument = strings.Join(fields[1:], " ")
	}
	return command, argument, true
}

func firstUnanswered(questions []mdast.Question) int {
	for index, question := range questions {
		if strings.TrimSpace(question.Answer) == "" {
			return index
		}
	}
	return len(questions)
}

func nextUnanswered(questions []mdast.Question, start int) int {
	if len(questions) == 0 {
		return -1
	}
	for offset := 0; offset < len(questions); offset++ {
		index := (start + offset) % len(questions)
		if strings.TrimSpace(questions[index].Answer) == "" {
			return index
		}
	}
	return -1
}

func countAnswered(questions []mdast.Question) int {
	count := 0
	for _, question := range questions {
		if strings.TrimSpace(question.Answer) != "" {
			count++
		}
	}
	return count
}

func trimContext(context []string) []string {
	const maximumCharacters = 1400
	const maximumItems = 8

	selected := append([]string(nil), context...)
	if len(selected) > maximumItems {
		selected = selected[len(selected)-maximumItems:]
	}

	total := 0
	result := make([]string, 0, len(selected))
	for _, item := range selected {
		if total+len(item) > maximumCharacters && len(result) > 0 {
			break
		}
		result = append(result, item)
		total += len(item)
	}
	return result
}

func sortedQuestionAnswers(questions []mdast.Question) []mdast.Question {
	copied := append([]mdast.Question(nil), questions...)
	sort.SliceStable(copied, func(left int, right int) bool {
		return copied[left].ID < copied[right].ID
	})
	return copied
}
