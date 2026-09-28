package cli

import (
	"errors"
	"flag"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Flags adapts Go's ordinary typed flag bindings to an owned argument struct.
// Configure runs for every parse against a fresh A and FlagSet; bind defaults
// there rather than retaining pointers or using global flags. Validation runs
// after parsing, before command construction. Positional arguments are rejected;
// a custom Decoder can explicitly describe positional/subcommand behavior.
func Flags[A any](configure func(*flag.FlagSet, *A), validate func(A) error) Decoder[A] {
	return func(args []string, help io.Writer) (A, error) {
		var result A
		if configure == nil || help == nil {
			return result, fault.New(fault.Invalid, "CLI flags require configuration and help output")
		}
		if err := validateArguments(args); err != nil {
			return result, err
		}
		flags := flag.NewFlagSet("command", flag.ContinueOnError)
		flags.SetOutput(help)
		configure(flags, &result)
		if err := ParseFlags(flags, args); err != nil {
			return *new(A), err
		}
		if flags.NArg() != 0 {
			return *new(A), Usage("command accepts flags only")
		}
		if validate != nil {
			if err := validate(result); err != nil {
				return *new(A), &UsageError{cause: err}
			}
		}
		return result, nil
	}
}

// ParseFlags shares usage classification and help-output error handling with
// development commands using an existing FlagSet. It does not change defaults
// or flag ownership; use a fresh FlagSet for each invocation.
func ParseFlags(flags *flag.FlagSet, args []string) error {
	if flags == nil {
		return fault.New(fault.Invalid, "CLI parsing requires a flag set")
	}
	if err := validateArguments(args); err != nil {
		return err
	}
	destination := flags.Output()
	output := &helpWriter{destination: destination}
	flags.SetOutput(output)
	defer flags.SetOutput(destination)
	err := flags.Parse(args)
	if output.failure != nil {
		return output.failure
	}
	return InvalidArguments(err)
}

type helpWriter struct {
	destination io.Writer
	failure     error
}

func (w *helpWriter) Write(p []byte) (int, error) {
	if w.failure != nil {
		return 0, w.failure
	}
	n, err := w.destination.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.failure = err
	return n, err
}

// UsageError distinguishes malformed input from an execution failure. Error
// causes are retained; a parser must avoid embedding credentials in messages.
type UsageError struct{ cause error }

func (e *UsageError) Error() string {
	if e == nil || e.cause == nil {
		return "invalid CLI usage"
	}
	return e.cause.Error()
}
func (e *UsageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
func Usage(message string) error { return &UsageError{cause: errors.New(message)} }

// InvalidArguments retains a custom parser/validator's cause with usage status.
// A clean help result remains clean help; an output failure should be returned
// directly rather than wrapped as an argument problem.
func InvalidArguments(err error) error {
	if err == nil || err == flag.ErrHelp {
		return err
	}
	return &UsageError{cause: err}
}
