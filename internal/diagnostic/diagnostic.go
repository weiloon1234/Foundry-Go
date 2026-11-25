// Package diagnostic is the one finding shape of every compiler-independent check
// the foundry command and the framework's documentation gate print:
//
//	path:line:col: [rule] subject: problem; fix. Read docs/guides/<guide>.md#<anchor>
//
// A rule owns the guide and explicit anchor that teach its fix. Line and column
// are omitted when a check has none; a zero is never printed.
package diagnostic

import (
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Diagnostic is one located finding. Skills names the application skills that
// declare the rule's guide anchor; it is empty outside an application.
type Diagnostic struct {
	Rule    Rule     `json:"rule"`
	Path    string   `json:"path"`
	Line    int      `json:"line,omitempty"`
	Column  int      `json:"column,omitempty"`
	Subject string   `json:"subject,omitempty"`
	Problem string   `json:"problem"`
	Fix     string   `json:"fix,omitempty"`
	Skills  []string `json:"skills,omitempty"`
}

// At starts a finding of rule at path. Line and column are one-based; zero omits them.
func (r Rule) At(path string, line, column int) Diagnostic {
	return Diagnostic{Rule: r, Path: path, Line: line, Column: column}
}

// Say sets the subject and the problem. The problem is lowercase text without a final period.
func (d Diagnostic) Say(subject, problem string) Diagnostic {
	d.Subject, d.Problem = subject, problem
	return d
}

// To sets the imperative fix.
func (d Diagnostic) To(fix string) Diagnostic {
	d.Fix = fix
	return d
}

// Text sets the problem and the fix from one "problem; fix" sentence, split at the
// first "; ". Text without that separator is all problem.
func (d Diagnostic) Text(subject, sentence string) Diagnostic {
	d.Subject = subject
	d.Problem, d.Fix, _ = strings.Cut(sentence, "; ")
	return d
}

// Location is path, path:line or path:line:col.
func (d Diagnostic) Location() string {
	location := d.Path
	if d.Line > 0 {
		location += ":" + strconv.Itoa(d.Line)
		if d.Column > 0 {
			location += ":" + strconv.Itoa(d.Column)
		}
	}
	return location
}

// String is the one printed shape.
func (d Diagnostic) String() string {
	var text strings.Builder
	text.WriteString(d.Location())
	text.WriteString(": [")
	text.WriteString(d.Rule.ID)
	text.WriteString("] ")
	if d.Subject != "" {
		text.WriteString(d.Subject)
		text.WriteString(": ")
	}
	text.WriteString(strings.TrimSuffix(d.Problem, "."))
	if d.Fix != "" {
		text.WriteString("; ")
		text.WriteString(strings.TrimSuffix(d.Fix, "."))
	}
	text.WriteString(". Read ")
	text.WriteString(d.Rule.Teaching())
	if len(d.Skills) > 0 {
		text.WriteString(" (skill: ")
		text.WriteString(strings.Join(d.Skills, ", "))
		text.WriteString(")")
	}
	return text.String()
}

// Error lets a finding travel as an error; use Err at a construction site.
func (d Diagnostic) Error() string { return d.String() }

// Err is the finding as a framework-owned error that keeps the value for Findings.
func (d Diagnostic) Err() error { return safeerror.Owned(d.String(), d) }

// Finding lets Findings recover a finding from an error that also carries a cause.
func (d Diagnostic) Finding() Diagnostic { return d }

// Caused is the finding as a framework-owned error that also keeps a cause for errors.Is.
func (d Diagnostic) Caused(cause error) error { return safeerror.Owned(d.String(), caused{d, cause}) }

type caused struct {
	Diagnostic
	cause error
}

func (c caused) Unwrap() error { return c.cause }

// Findings returns every Diagnostic in err in order, and the text of every other
// owned failure it joins as a Diagnostic-free line. Traversal is bounded by depth.
func Findings(err error) (found []Diagnostic, other []string) {
	var walk func(error, int)
	walk = func(err error, depth int) {
		if err == nil {
			return
		}
		if depth > maxDepth {
			other = append(other, "diagnostic nesting is too deep to list")
			return
		}
		inner := safeerror.UnwrapOwned(err)
		switch value := inner.(type) {
		case interface{ Finding() Diagnostic }:
			found = append(found, value.Finding())
		case interface{ Unwrap() []error }:
			for _, child := range value.Unwrap() {
				walk(child, depth+1)
			}
		default:
			if message, ok := safeerror.OwnedMessage(err); ok {
				other = append(other, message)
			} else {
				other = append(other, "check failed")
			}
		}
	}
	walk(err, 0)
	return found, other
}

const maxDepth = 32

// Annotate adds the skills that teach each finding of err. The skills are read
// only when err has findings. The result's text is one finding per line, and err
// stays its cause. An error with no findings is returned unchanged.
func Annotate(err error, skills func() Skills) error {
	found, other := Findings(err)
	if len(found) == 0 {
		return err
	}
	lines := Lines(found, skills())
	lines = append(lines, other...)
	return safeerror.Owned(strings.Join(lines, "\n"), err)
}

// WithSkills names the skills that declare the rule's guide anchor.
func (d Diagnostic) WithSkills(skills Skills) Diagnostic {
	d.Skills = skills.Names(d.Rule)
	return d
}

// Join returns the findings as one error, or nil for none.
func Join(findings []Diagnostic) error {
	failures := make([]error, len(findings))
	for i, d := range findings {
		failures[i] = d.Err()
	}
	return safeerror.Join("check failed", failures...)
}

// Lines prints each finding on its own line with the skills that teach it.
func Lines(findings []Diagnostic, skills Skills) []string {
	lines := make([]string, len(findings))
	for i, d := range findings {
		lines[i] = d.WithSkills(skills).String()
	}
	return lines
}
