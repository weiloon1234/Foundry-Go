package docsource

import (
	"flag"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"io"
	"regexp"
	"strings"
)

var filesystemFailure = regexp.MustCompile(`^(?:open|read|stat|lstat|mkdir|write|readdir) ([^:]+): (.*)$`)

var diagnosticLocation = regexp.MustCompile(`^([^:\n]+):(?:(\d+):)?\s*(.*)$`)

// Diagnostics preserves reader locations and terminates each diagnostic consistently.
// Line zero identifies a command or filesystem error without a source line.
func Diagnostics(err error) string {
	if err == nil {
		return ""
	}
	lines := strings.Split(err.Error(), "\n")
	for i, line := range lines {
		location, problem := "documentation:0", line
		if match := filesystemFailure.FindStringSubmatch(line); match != nil {
			location, problem = match[1]+":0", match[2]+"; restore the file and its required access permissions"
		} else if match := diagnosticLocation.FindStringSubmatch(line); match != nil {
			number := match[2]
			if number == "" {
				number = "0"
			}
			location, problem = match[1]+":"+number, match[3]
		}
		lines[i] = location + ": " + strings.TrimSuffix(problem, ".") + "."
	}
	return strings.Join(lines, "\n")
}

// ParseFlags reports invalid command arguments without inventing a document location.
func ParseFlags(flags *flag.FlagSet, args []string) error {
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		var names []string
		flags.VisitAll(func(f *flag.Flag) { names = append(names, "--"+f.Name) })
		return safeerror.Owned(fmt.Sprintf("%s:0: invalid flags; use the supported flags: %s.", flags.Name(), strings.Join(names, ", ")), err)
	}
	if flags.NArg() != 0 {
		return safeerror.New(fmt.Sprintf("%s:0: unexpected positional argument %q; pass options as named flags.", flags.Name(), flags.Arg(0)))
	}
	return nil
}
