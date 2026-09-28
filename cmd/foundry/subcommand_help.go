package main

import (
	"flag"
	"fmt"
	"io"
)

// Subcommand groups print their usage before an operation is selected. Preserve
// the same clean-help and failed-writer semantics as ordinary flag parsing.
func subcommandHelp(args []string, output io.Writer, usage string) (bool, error) {
	if len(args) != 1 || (args[0] != "--help" && args[0] != "-h") {
		return false, nil
	}
	if _, err := fmt.Fprintln(output, usage); err != nil {
		return true, err
	}
	return true, flag.ErrHelp
}
