package apicomment

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

// StagedFile lists, relative to the framework root, the declarations that need
// a comment and still lack one. It only shrinks: the check refuses a missing
// comment that is not listed (new API is commented) and a listed declaration
// that is now commented, gone or no longer required. The target is an empty
// file. It lives beside the check, not in the exported-API record, because that
// record is regenerated from the source and this file is a hand-reviewed debt
// list that no generator may grow.
const StagedFile = "internal/apicomment/uncommented.txt"

// StagedHeader starts the file; every other line is "package\tkind\tname".
const StagedHeader = `# Declarations that need a doc comment and still lack one, one per line:
# package, kind (const, var or field) and name, separated by tabs, sorted.
# Checked by internal/cmd/apicomments (make docs-check). The file only shrinks:
# write the comment, then run go run ./internal/cmd/apicomments --shrink.
# The release target is an empty file; the line count is reported at each release.
`

// Staged is the parsed staging file.
type Staged struct {
	// Lines holds each listed key with its one-based line number.
	Lines map[string]int
	// Order is the keys in file order.
	Order []string
	// Findings are the file's own faults: a malformed, duplicate or unsorted line.
	Findings []diagnostic.Diagnostic
}

// ParseStaged reads the staging file's bytes.
func ParseStaged(data []byte) Staged {
	staged := Staged{Lines: make(map[string]int)}
	previous := ""
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		at := func(problem, fix string) diagnostic.Diagnostic {
			return diagnostic.APIComment.At(StagedFile, i+1, 0).Say("", problem).To(fix)
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || (fields[1] != KindConst && fields[1] != KindVar && fields[1] != KindField) || fields[0] == "" || fields[2] == "" {
			staged.Findings = append(staged.Findings, at("line is not \"package\", kind and name separated by tabs", "remove the line"))
			continue
		}
		if _, dup := staged.Lines[line]; dup {
			staged.Findings = append(staged.Findings, at("line repeats an earlier line", "remove the duplicate"))
			continue
		}
		if line < previous {
			staged.Findings = append(staged.Findings, at("line is out of order", "keep the lines sorted"))
		}
		previous = line
		staged.Lines[line] = i + 1
		staged.Order = append(staged.Order, line)
	}
	return staged
}

// Compare judges the analysed declarations against the staging file. Every
// finding is something to fix by editing a source file or by removing a line
// of the staging file; none asks for a line to be added.
func Compare(declarations []Declaration, staged Staged) []diagnostic.Diagnostic {
	findings := append([]diagnostic.Diagnostic(nil), staged.Findings...)
	required := make(map[string]Declaration, len(declarations))
	for _, d := range declarations {
		required[d.Key()] = d
		if d.Commented() {
			continue
		}
		if _, listed := staged.Lines[d.Key()]; listed {
			continue
		}
		findings = append(findings, diagnostic.APIComment.At(d.File, d.Line, 0).Say(d.Package+"."+d.Name+" ("+d.Kind+")", d.Problem).To(fixFor(d)))
	}
	for _, key := range staged.Order {
		line := staged.Lines[key]
		d, ok := required[key]
		name := strings.ReplaceAll(key, "\t", " ")
		switch {
		case !ok:
			findings = append(findings, diagnostic.APIComment.At(StagedFile, line, 0).Say(name, "is listed but is not a declaration that needs a comment (it is gone or no longer a caller input)").To(removeLine))
		case d.Commented():
			findings = append(findings, diagnostic.APIComment.At(StagedFile, line, 0).Say(name, "is listed but now has an acceptable comment").To(removeLine))
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings
}

const removeLine = "remove the line (go run ./internal/cmd/apicomments --shrink does it)"

func fixFor(d Declaration) string {
	name := d.Name[strings.LastIndex(d.Name, ".")+1:]
	if d.Kind != KindField {
		name = d.Name
	}
	switch d.Problem {
	case problemMulti:
		return "declare each name on its own line with its own comment"
	case problemRestating:
		return "say what " + name + " is for, not only its name"
	}
	return "add a comment that starts with \"" + name + "\" and says what it is for; new API is not staged"
}

// Shrink returns the staging file with every line that Compare would call
// stale removed. It never adds a line: the result's lines are a subset of the
// input's, in the input's order.
func Shrink(declarations []Declaration, data []byte) []byte {
	required := make(map[string]Declaration, len(declarations))
	for _, d := range declarations {
		required[d.Key()] = d
	}
	var out strings.Builder
	out.WriteString(StagedHeader)
	seen := make(map[string]bool)
	for _, key := range ParseStaged(data).Order {
		if d, ok := required[key]; ok && !d.Commented() && !seen[key] {
			seen[key] = true
			out.WriteString(key + "\n")
		}
	}
	return []byte(out.String())
}

// Check analyses root and judges it against its staging file. The count is the
// number of listed declarations.
func Check(root string) (findings []diagnostic.Diagnostic, count int, err error) {
	declarations, err := Analyze(root)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(filepath.Join(root, StagedFile))
	if err != nil {
		return nil, 0, err
	}
	staged := ParseStaged(data)
	return Compare(declarations, staged), len(staged.Order), nil
}

// CountText is the one-line report of the staging file's size.
func CountText(count int) string {
	return "api-comment: " + strconv.Itoa(count) + " declarations are staged without a comment (target 0)"
}
