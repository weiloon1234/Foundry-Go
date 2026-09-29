package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Flags adapts Go's ordinary typed flag bindings to an owned argument struct.
// Configure runs for every parse against a fresh A and FlagSet; bind defaults
// there rather than retaining pointers or using global flags. Validation runs
// after parsing, before command construction. Positional arguments are rejected;
// use FlagsWithArgs, or a custom Decoder for subcommand behavior.
func Flags[A any](configure func(*flag.FlagSet, *A), validate func(A) error) Decoder[A] {
	return FlagsWithArgs(configure, nil, validate)
}

// FlagsWithArgs also binds the positional arguments remaining after flags
// (and after a "--" terminator) into the same owned argument struct. The
// positional callback receives an owned slice; return an error for a wrong count
// or format, which becomes a usage error. A nil callback rejects positional
// arguments exactly like Flags.
func FlagsWithArgs[A any](configure func(*flag.FlagSet, *A), positional func(*A, []string) error, validate func(A) error) Decoder[A] {
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
		if positional != nil {
			if err := positional(&result, slices.Clone(flags.Args())); err != nil {
				return *new(A), InvalidArguments(err)
			}
		} else if flags.NArg() != 0 {
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

// ExactArgs binds exactly count positional values through assign, a helper for
// FlagsWithArgs. Values are validated text; assign converts them to typed fields.
func ExactArgs[A any](count int, assign func(*A, []string) error) func(*A, []string) error {
	return func(target *A, values []string) error {
		if count < 0 || assign == nil {
			return fault.New(fault.Invalid, "positional binding requires a count and assignment")
		}
		if len(values) != count {
			return fmt.Errorf("expected %d positional argument(s), got %d", count, len(values))
		}
		return assign(target, values)
	}
}
