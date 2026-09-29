package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ErrNotConfirmed reports that an operator declined, or could not be asked to
// confirm, a destructive command. It is an ordinary failure (exit status 1).
var ErrNotConfirmed = errors.New("operation was not confirmed; rerun with --force to proceed")

const maxAnswerBytes = 256

// Confirm writes prompt to Err and reads one answer line from In. Only "y" or
// "yes" (any case) confirms. force proceeds without prompting. An input that is
// an *os.File but not a terminal (a pipe, file or /dev/null) is non-interactive
// and refuses without reading. Reading is synchronous: a blocked terminal read
// is not interrupted by ctx, which is checked before and after the answer.
func Confirm(ctx context.Context, streams Streams, prompt string, force bool) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "confirmation requires a context")
	}
	if err := streams.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if force {
		return nil
	}
	if !interactive(streams.In) {
		return ErrNotConfirmed
	}
	if prompt == "" || strings.IndexFunc(prompt, unicode.IsControl) >= 0 || !utf8.ValidString(prompt) {
		return fault.New(fault.Invalid, "confirmation prompt must be single-line text")
	}
	if _, err := fmt.Fprintf(streams.Err, "%s [y/N]: ", prompt); err != nil {
		return err
	}
	answer, err := readLine(streams.In)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return ErrNotConfirmed
	}
}

// ConfirmInProduction confirms only when production is true, typically from the
// application's configured environment. Other environments proceed silently.
func ConfirmInProduction(ctx context.Context, streams Streams, production bool, prompt string, force bool) error {
	if !production {
		if ctx == nil {
			return fault.New(fault.Invalid, "confirmation requires a context")
		}
		return ctx.Err()
	}
	return Confirm(ctx, streams, prompt, force)
}

func interactive(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// readLine reads byte by byte so no input beyond the answer is consumed.
func readLine(input io.Reader) (string, error) {
	var line []byte
	var buffer [1]byte
	for len(line) < maxAnswerBytes {
		n, err := input.Read(buffer[:])
		if n == 1 {
			if buffer[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buffer[0])
		}
		if err != nil {
			return string(line), err
		}
	}
	return "", fault.New(fault.Invalid, "confirmation answer exceeds its bound")
}

// WriteTable writes aligned, tab-separated columns. Every row must have the
// header's column count. Tabs, newlines and other control characters inside
// cells become spaces, so application data cannot forge extra rows or columns.
func WriteTable(output io.Writer, header []string, rows [][]string) error {
	if output == nil || len(header) == 0 || len(header) > 64 || len(rows) > 1<<16 {
		return fault.New(fault.Invalid, "table requires an output and bounded columns and rows")
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	line := func(cells []string) error {
		if len(cells) != len(header) {
			return fault.New(fault.Invalid, "table row does not match its header")
		}
		clean := make([]string, len(cells))
		for i, cell := range cells {
			clean[i] = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) || r == utf8.RuneError {
					return ' '
				}
				return r
			}, cell)
		}
		_, err := fmt.Fprintln(writer, strings.Join(clean, "\t"))
		return err
	}
	if err := line(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := line(row); err != nil {
			return err
		}
	}
	return writer.Flush()
}
