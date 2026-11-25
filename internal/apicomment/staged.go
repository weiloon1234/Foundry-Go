package apicomment

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// StagedFile lists, relative to the framework root, the declarations that need
// a comment and still lack one. It only shrinks: the check refuses a missing
// comment that is not listed (new API is commented) and a listed declaration
// that is now commented, gone or no longer required. The target is an empty
// file. It lives beside the check, not in the exported-API record, because that
// record is regenerated from the source and this file is a hand-reviewed debt
// list that no generator may grow.
const StagedFile = "internal/apicomment/uncommented.txt"

// RecordedFile is the copy of StagedFile as of the last release. The staging
// file may hold only lines of this copy, so a line added by hand fails: new API
// is commented, never staged. `apicomments --release vX.Y.Z` rewrites it from
// the staging file in the release commit (docs/release-checklist.md).
const RecordedFile = "internal/apicomment/release/uncommented.txt"

// StagedHeader starts the file; every other line is "package\tkind\tname".
const StagedHeader = `# Declarations that need a doc comment and still lack one, one per line:
# package, kind (const, var or field) and name, separated by tabs, sorted.
# Checked by internal/cmd/apicomments (make docs-check). The file only shrinks:
# write the comment, then run go run ./internal/cmd/apicomments --shrink.
# The file may hold only lines of the copy recorded at the last release
# (internal/apicomment/release/uncommented.txt); a line added by hand fails.
# The release target is an empty file; the line count is reported at each release.
# The first recorded copy is a starting point written under the label of the newest
# released version; the next go run ./internal/cmd/apicomments --release vX.Y.Z replaces it.
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
		if strings.HasSuffix(line, "\r") {
			staged.Findings = append(staged.Findings, at("line ends with a carriage return", "save the file with Unix line endings (go run ./internal/cmd/apicomments --shrink rewrites it)"))
			continue
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

// Recorded is the staging file as recorded at the last release.
type Recorded struct {
	// Version is the release tag from the first line, empty when it has none.
	Version string
	// Lines holds the recorded keys.
	Lines map[string]int
}

// ParseRecorded reads the recorded copy's bytes. Its own faults are not
// reported: a line that is not valid can never match a staging line.
func ParseRecorded(data []byte) Recorded {
	version, _ := retirement.ReleaseVersion(data)
	return Recorded{Version: version, Lines: ParseStaged(data).Lines}
}

// RecordedCopy is the bytes `--release` writes: the release line, then the
// staging file as it stands.
func RecordedCopy(staged []byte, tag string) []byte {
	return append([]byte("# Recorded at release "+tag+".\n"), staged...)
}

// Compare judges the analysed declarations against the staging file and the
// copy recorded at the last release. Every finding is something to fix by
// editing a source file or by removing a line of the staging file; none asks
// for a line to be added.
func Compare(declarations []Declaration, staged Staged, recorded Recorded) []diagnostic.Diagnostic {
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
		if _, kept := recorded.Lines[key]; !kept {
			findings = append(findings, diagnostic.APIComment.At(StagedFile, line, 0).Say(name, "is not in the copy recorded at release "+recorded.Version+" ("+RecordedFile+"), so it was added after that release; a declaration added after a release must have its comment, because the staging file can only shrink").To("remove the line and comment the declaration"))
		}
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
// stale removed, sorted and without duplicates. It never adds a line: the
// result's lines are a subset of the input's.
func Shrink(declarations []Declaration, data []byte) []byte {
	required := make(map[string]Declaration, len(declarations))
	for _, d := range declarations {
		required[d.Key()] = d
	}
	var out strings.Builder
	out.WriteString(StagedHeader)
	seen := make(map[string]bool)
	var kept []string
	for _, key := range ParseStaged(data).Order {
		if d, ok := required[key]; ok && !d.Commented() && !seen[key] {
			seen[key] = true
			kept = append(kept, key)
		}
	}
	sort.Strings(kept)
	for _, key := range kept {
		out.WriteString(key + "\n")
	}
	return []byte(out.String())
}

// Check analyses root and judges it against its staging file and the recorded
// copy. The count is the number of listed declarations.
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
	recorded, err := os.ReadFile(filepath.Join(root, RecordedFile))
	if err != nil {
		return nil, 0, err
	}
	return Compare(declarations, staged, ParseRecorded(recorded)), len(staged.Order), nil
}

// Release writes the recorded copy for tag from the staging file, once the
// check passes: the copy is the starting point the next release's staging file
// may only shrink from.
func Release(root, tag string) error {
	if !retirement.ValidReleaseTag(tag) {
		return safeerror.New("--release " + tag + ": want a tag of the form vMAJOR.MINOR.PATCH, for example v0.53.0")
	}
	findings, _, err := Check(root)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return diagnostic.Join(findings)
	}
	data, err := os.ReadFile(filepath.Join(root, StagedFile))
	if err != nil {
		return err
	}
	path := filepath.Join(root, RecordedFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, RecordedCopy(data, tag), 0o644)
}

// CountText is the one-line report of the staging file's size.
func CountText(count int) string {
	return "api-comment: " + strconv.Itoa(count) + " declarations are staged without a comment (target 0)"
}
