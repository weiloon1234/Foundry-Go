package docsource

import (
	"flag"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"io"
	"regexp"
	"strings"
)

var filesystemFailure = regexp.MustCompile(`^(?:open|read|stat|lstat|mkdir|write|readdir) ([^:]+): (.*)$`)

// Diagnostics prints one failure per line. Findings already carry their location,
// rule and teaching reference; a bare filesystem failure becomes a doc-file finding.
// Stages that read the same document report its refusal each time, so an identical
// line is printed once.
func Diagnostics(err error) string {
	if err == nil {
		return ""
	}
	var lines []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(err.Error(), "\n") {
		if match := filesystemFailure.FindStringSubmatch(line); match != nil {
			line = diagnostic.DocFile.At(match[1], 0, 0).Say("input", match[2]).To("restore the file and its required access permissions").String()
		}
		if !seen[line] {
			seen[line] = true
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// ParseFlags reports invalid command arguments without inventing a document location.
func ParseFlags(flags *flag.FlagSet, args []string) error {
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		var names []string
		flags.VisitAll(func(f *flag.Flag) { names = append(names, "--"+f.Name) })
		return safeerror.Owned(fmt.Sprintf("%s: invalid flags; use the supported flags: %s.", flags.Name(), strings.Join(names, ", ")), err)
	}
	if flags.NArg() != 0 {
		return safeerror.New(fmt.Sprintf("%s: unexpected positional argument %q; pass options as named flags.", flags.Name(), flags.Arg(0)))
	}
	return nil
}
