package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type Navigation int

const (
	NavigationNone Navigation = iota
	NavigationPrevious
	NavigationNext
)

type Result struct {
	Line       string
	Navigation Navigation
}

type Reader struct {
	buffered *bufio.Reader
	file     *os.File
	output   io.Writer
	restore  func() error
	raw      bool
}

func New(input io.Reader, buffered *bufio.Reader, output io.Writer) *Reader {
	reader := &Reader{
		buffered: buffered,
		output:   output,
	}

	file, ok := input.(*os.File)
	if !ok || buffered.Buffered() != 0 {
		return reader
	}

	restore, enabled := enableRawMode(file)
	if !enabled {
		return reader
	}

	reader.file = file
	reader.restore = restore
	reader.raw = true
	return reader
}

func (reader *Reader) Close() error {
	if reader.restore == nil {
		return nil
	}
	return reader.restore()
}

func (reader *Reader) ReadPrompt() (Result, error) {
	if reader.raw {
		return reader.readRaw(true)
	}

	line, err := readLine(reader.buffered)
	if err != nil {
		return Result{}, err
	}
	return classifyLine(line, true), nil
}

func (reader *Reader) ReadLine() (string, error) {
	if reader.raw {
		result, err := reader.readRaw(false)
		return result.Line, err
	}
	return readLine(reader.buffered)
}

func classifyLine(line string, navigationEnabled bool) Result {
	if !navigationEnabled {
		return Result{Line: line}
	}

	switch line {
	case "\x1b[A", "\x1b[D", "\x1bOA", "\x1bOD":
		return Result{Navigation: NavigationPrevious}
	case "\x1b[B", "\x1b[C", "\x1bOB", "\x1bOC":
		return Result{Navigation: NavigationNext}
	default:
		return Result{Line: line}
	}
}

func (reader *Reader) readRaw(navigationEnabled bool) (Result, error) {
	runes := make([]rune, 0, 64)

	for {
		first, err := reader.readByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(runes) > 0 {
				fmt.Fprintln(reader.output)
				return Result{Line: string(runes)}, nil
			}
			return Result{}, err
		}

		switch first {
		case '\r', '\n':
			fmt.Fprintln(reader.output)
			return Result{Line: string(runes)}, nil
		case 0x03:
			fmt.Fprintln(reader.output, "^C")
			return Result{}, ErrInterrupted
		case 0x04:
			if len(runes) == 0 {
				return Result{}, io.EOF
			}
		case 0x08, 0x7f:
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
				fmt.Fprint(reader.output, "\b \b")
			}
		case 0x1b:
			sequence, err := reader.readEscapeSequence()
			if err != nil {
				return Result{}, err
			}
			if !navigationEnabled || len(runes) > 0 {
				continue
			}
			switch sequence {
			case "[A", "[D", "OA", "OD":
				fmt.Fprintln(reader.output)
				return Result{Navigation: NavigationPrevious}, nil
			case "[B", "[C", "OB", "OC":
				fmt.Fprintln(reader.output)
				return Result{Navigation: NavigationNext}, nil
			}
		default:
			r, text, err := reader.decodeRune(first)
			if err != nil {
				return Result{}, err
			}
			if r < 0x20 || r == 0x7f {
				continue
			}
			runes = append(runes, r)
			fmt.Fprint(reader.output, text)
		}
	}
}

func (reader *Reader) readEscapeSequence() (string, error) {
	second, err := reader.readByte()
	if err != nil {
		return "", err
	}
	if second != '[' && second != 'O' {
		return string(second), nil
	}

	third, err := reader.readByte()
	if err != nil {
		return "", err
	}
	return string([]byte{second, third}), nil
}

func (reader *Reader) decodeRune(first byte) (rune, string, error) {
	if first < utf8.RuneSelf {
		return rune(first), string(first), nil
	}

	length := utf8SequenceLength(first)
	if length == 0 {
		return utf8.RuneError, "", nil
	}

	encoded := make([]byte, length)
	encoded[0] = first
	if _, err := io.ReadFull(reader.file, encoded[1:]); err != nil {
		return 0, "", err
	}
	r, size := utf8.DecodeRune(encoded)
	if r == utf8.RuneError && size == 1 {
		return r, "", nil
	}
	return r, string(encoded), nil
}

func utf8SequenceLength(first byte) int {
	switch {
	case first&0xe0 == 0xc0:
		return 2
	case first&0xf0 == 0xe0:
		return 3
	case first&0xf8 == 0xf0:
		return 4
	default:
		return 0
	}
}

func (reader *Reader) readByte() (byte, error) {
	var buffer [1]byte
	_, err := reader.file.Read(buffer[:])
	if err != nil {
		return 0, err
	}
	return buffer[0], nil
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

var ErrInterrupted = errors.New("terminal input interrupted")
