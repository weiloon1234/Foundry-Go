package apicomment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

func writeFile(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRoot is a framework tree with one package, kit, holding source.
func newRoot(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module github.com/weiloon1234/Foundry-Go\n\ngo 1.22\n")
	writeFile(t, root, "kit/kit.go", source)
	return root
}

// analyse returns each declaration's key to its problem ("" for a comment that is accepted).
func analyse(t *testing.T, source string) map[string]string {
	t.Helper()
	declarations, err := Analyze(newRoot(t, source))
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string)
	for _, d := range declarations {
		result[d.Kind+" "+d.Name] = d.Problem
	}
	return result
}

func TestCallerInputFieldsNeedComments(t *testing.T) {
	got := analyse(t, `package kit

// Spec is what Define takes.
type Spec struct {
	// Name labels the spec in listings.
	Name string
	Size int
	Inner Options
	Hook func(Event) Verdict
	// Result is not a caller input here but is reached through Spec.
	Nested *Nested
}

type Options struct {
	Mode string // Mode selects how the spec runs.
}

// Event reaches the application, which does not supply it.
type Event struct{ Detail string }

type Verdict struct{ Allow bool }

type Nested struct{ Depth int }

// Define declares a spec.
func Define(spec Spec) {}

// Output only appears in results.
type Output struct{ Value string }

// Run returns an output.
func Run() Output { return Output{} }
`)
	want := map[string]string{
		"field Spec.Name":     "",
		"field Spec.Size":     problemMissing,
		"field Spec.Inner":    problemMissing,
		"field Spec.Hook":     problemMissing,
		"field Spec.Nested":   problemName,
		"field Options.Mode":  "",
		"field Verdict.Allow": problemMissing,
		"field Nested.Depth":  problemMissing,
	}
	for key, problem := range want {
		if got[key] != problem {
			t.Errorf("%s: got %q, want %q", key, got[key], problem)
		}
	}
	for _, key := range []string{"field Output.Value", "field Event.Detail"} {
		if _, present := got[key]; present {
			t.Errorf("%s: a struct that only the framework supplies is not required", key)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declarations: %v", got)
	}
}

func TestConstantsAndVariablesNeedOwnComments(t *testing.T) {
	got := analyse(t, `package kit

// Mode values.
const (
	// Fast skips checks.
	Fast = "fast"
	Slow = "slow"
	Plain = "plain" // Plain is the default mode for callers.
)

// Limit is the largest size a spec may have.
const Limit = 10

var Default = 3

const A, B = 1, 2
`)
	want := map[string]string{
		"const Fast":  "",
		"const Slow":  problemMissing,
		"const Plain": "",
		"const Limit": "",
		"var Default": problemMissing,
		"const A":     problemMulti,
		"const B":     problemMulti,
	}
	for key, problem := range want {
		if got[key] != problem {
			t.Errorf("%s: got %q, want %q", key, got[key], problem)
		}
	}
}

func TestRestatingAndUnnamedCommentsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		typeNames  []string
		want       string
	}{
		{"Timeout", "Timeout is the timeout.", nil, problemRestating},
		{"RetryLimit", "RetryLimit is the retry limit", nil, problemRestating},
		{"Name", "Name.", nil, problemRestating},
		{"Timeout", "Timeout is how long a request may run.", nil, ""},
		{"ID", "ID identifies the route.", nil, ""},
		{"Timeout", "The timeout for requests.", nil, problemName},
		{"Timeouts", "Timeout lists them.", nil, problemName},
		{"Max", "Maximum rows.", nil, problemName},
		// The whole comment counts, not the first sentence.
		{"Timeout", "Timeout is the timeout. Zero means no limit.", nil, ""},
		{"Name", "Name is the name. It is shown in listings.", nil, ""},
		// Filler words and the declaration's own type say nothing.
		{"Name", "Name is the name value.", nil, problemRestating},
		{"Name", "Name is the name of the item.", nil, ""},
		{"Timeout", "Timeout is the timeout setting.", nil, problemRestating},
		{"Timeout", "Timeout is the timeout option.", nil, problemRestating},
		{"Name", "Name is the name field parameter flag.", nil, problemRestating},
		{"Name", "Name TODO", nil, problemRestating},
		{"Name", "Name is a string.", []string{"string"}, problemRestating},
		{"Enabled", "Enabled is a bool flag.", []string{"bool"}, problemRestating},
		{"Timeout", "Timeout is the timeout duration.", []string{"time", "duration"}, problemRestating},
		{"Settings", "Settings are the settings values.", nil, problemRestating},
		{"Name", "Name is the string shown to users.", []string{"string"}, ""},
	} {
		if got := judgeText(tc.text, tc.name, tc.typeNames); got != tc.want {
			t.Errorf("%q for %s: got %q, want %q", tc.text, tc.name, got, tc.want)
		}
	}
}

// The words of the declaration's type come from go/types, so a field of type
// time.Duration cannot be described by saying it is a duration.
func TestTypeNameWordsComeFromTheDeclaredType(t *testing.T) {
	got := analyse(t, `package kit

import "time"

// Spec is what Define takes.
type Spec struct {
	// Timeout is the timeout duration.
	Timeout time.Duration
	// Wait is the longest a caller blocks, as a duration.
	Wait time.Duration
}

// Define declares a spec.
func Define(Spec) {}
`)
	if got["field Spec.Timeout"] != problemRestating || got["field Spec.Wait"] != "" {
		t.Fatalf("type words: %v", got)
	}
}

func decl(kind, name string, problem string) Declaration {
	return Declaration{Package: "kit", Kind: kind, Name: name, File: "kit/kit.go", Line: 3, Problem: problem}
}

func TestStagingFileRules(t *testing.T) {
	declarations := []Declaration{
		decl(KindField, "Spec.Listed", problemMissing),
		decl(KindField, "Spec.New", problemMissing),
		decl(KindField, "Spec.Fixed", ""),
	}
	text := StagedHeader + "kit\tfield\tSpec.Fixed\nkit\tfield\tSpec.Gone\nkit\tfield\tSpec.Listed\n"
	staged := ParseStaged([]byte(text))
	findings := Compare(declarations, staged, ParseRecorded(RecordedCopy([]byte(text), "v1.0.0")))
	var lines []string
	for _, f := range findings {
		lines = append(lines, f.String())
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{
		"kit/kit.go:3: [api-comment] kit.Spec.New (field): has no doc comment; add a comment that starts with \"New\"",
		"internal/apicomment/uncommented.txt:10: [api-comment] kit field Spec.Fixed: is listed but now has an acceptable comment; remove the line",
		"kit field Spec.Gone: is listed but is not a declaration that needs a comment",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, "Spec.Listed") || len(findings) != 3 {
		t.Errorf("a listed missing comment is allowed; findings:\n%s", all)
	}
}

func TestStagingFileFaults(t *testing.T) {
	staged := ParseStaged([]byte("kit\tfield\tB\nkit\tfield\tA\nkit\tfield\tA\nnot a line\nkit\tmethod\tX\n"))
	if len(staged.Findings) != 4 {
		t.Fatalf("out of order, duplicate and two malformed lines: %v", staged.Findings)
	}
}

func TestStagingLineWithCarriageReturnSaysSo(t *testing.T) {
	staged := ParseStaged([]byte("kit\tfield\tA\r\nkit\tfield\tB\n"))
	if len(staged.Findings) != 1 || !strings.Contains(staged.Findings[0].String(), "uncommented.txt:1: [api-comment] line ends with a carriage return; save the file with Unix line endings") {
		t.Fatalf("a carriage return is named: %v", staged.Findings)
	}
	if len(staged.Order) != 1 || staged.Order[0] != "kit\tfield\tB" {
		t.Fatalf("the faulty line is not kept: %v", staged.Order)
	}
}

// A line the recorded copy does not hold was added after the release. Adding a
// line for a real uncommented declaration used to pass; it fails now.
func TestHandAddedStagingLineFails(t *testing.T) {
	declarations := []Declaration{
		decl(KindField, "Spec.Old", problemMissing),
		decl(KindField, "Spec.Added", problemMissing),
	}
	recorded := ParseRecorded(RecordedCopy([]byte(StagedHeader+"kit\tfield\tSpec.Old\n"), "v1.2.3"))
	staged := ParseStaged([]byte(StagedHeader + "kit\tfield\tSpec.Added\nkit\tfield\tSpec.Old\n"))
	findings := Compare(declarations, staged, recorded)
	if len(findings) != 1 {
		t.Fatalf("one finding for the added line: %v", findings)
	}
	for _, want := range []string{
		"uncommented.txt:10: [api-comment] kit field Spec.Added: is not in the copy recorded at release v1.2.3",
		"a declaration added after a release must have its comment, because the staging file can only shrink",
		"remove the line and comment the declaration",
	} {
		if !strings.Contains(findings[0].String(), want) {
			t.Errorf("missing %q in %s", want, findings[0])
		}
	}
	if got := Compare(declarations[:1], ParseStaged([]byte(StagedHeader+"kit\tfield\tSpec.Old\n")), recorded); len(got) != 0 {
		t.Fatalf("a recorded line passes: %v", got)
	}
}

// The recorded copy belongs to the release commit: its release line is the
// newest released heading of the changelog, as the retired-names baseline's is.
func TestRecordedCopyIsTheNewestReleasedVersion(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join("../..", RecordedFile))
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile(filepath.Join("../..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := retirement.CheckRecordedVersion(RecordedFile, "go run ./internal/cmd/apicomments --release", recorded, changelog); err != nil {
		t.Fatal(err)
	}
	stale := []byte("# Changelog\n\n## Unreleased\n\n## v9.9.9 (2030-01-01)\n")
	err = retirement.CheckRecordedVersion(RecordedFile, "go run ./internal/cmd/apicomments --release", recorded, stale)
	if err == nil || !strings.Contains(err.Error(), "run go run ./internal/cmd/apicomments --release v9.9.9 in the release commit, with the heading") {
		t.Fatalf("the message names the release step: %v", err)
	}
}

func TestShrinkOnlyRemoves(t *testing.T) {
	declarations := []Declaration{
		decl(KindField, "Spec.Listed", problemMissing),
		decl(KindField, "Spec.Unlisted", problemMissing),
		decl(KindField, "Spec.Fixed", ""),
	}
	before := StagedHeader + "kit\tfield\tSpec.Fixed\nkit\tfield\tSpec.Gone\nkit\tfield\tSpec.Listed\n"
	after := string(Shrink(declarations, []byte(before)))
	if after != StagedHeader+"kit\tfield\tSpec.Listed\n" {
		t.Fatalf("shrunk file:\n%s", after)
	}
	if strings.Contains(after, "Unlisted") {
		t.Fatal("shrinking must never add a line")
	}
	if len(Compare(declarations[:1], ParseStaged([]byte(after)), ParseRecorded(RecordedCopy([]byte(before), "v1.0.0")))) != 0 {
		t.Fatal("the shrunk file must pass")
	}
	if again := string(Shrink(declarations, []byte(after))); again != after {
		t.Fatalf("shrinking is idempotent: %q", again)
	}
}

// The "out of order" finding names no fix of its own, so the command that fixes
// the file must sort it and drop a repeated line.
func TestShrinkSortsAndRemovesDuplicates(t *testing.T) {
	declarations := []Declaration{
		decl(KindField, "Spec.A", problemMissing),
		decl(KindField, "Spec.B", problemMissing),
		decl(KindField, "Spec.C", problemMissing),
	}
	before := StagedHeader + "kit\tfield\tSpec.C\nkit\tfield\tSpec.A\nkit\tfield\tSpec.C\nkit\tfield\tSpec.B\n"
	if got := ParseStaged([]byte(before)); len(got.Findings) == 0 {
		t.Fatal("the input is unsorted and repeats a line")
	}
	after := Shrink(declarations, []byte(before))
	if string(after) != StagedHeader+"kit\tfield\tSpec.A\nkit\tfield\tSpec.B\nkit\tfield\tSpec.C\n" {
		t.Fatalf("shrunk file:\n%s", after)
	}
	if staged := ParseStaged(after); len(staged.Findings) != 0 {
		t.Fatalf("the shrunk file has no faults of its own: %v", staged.Findings)
	}
}

func TestCheckEndToEnd(t *testing.T) {
	root := newRoot(t, "package kit\n\ntype Spec struct{ Name string }\n\nfunc Define(Spec) {}\n")
	writeFile(t, root, StagedFile, StagedHeader)
	writeFile(t, root, RecordedFile, string(RecordedCopy([]byte(StagedHeader+"kit\tfield\tSpec.Name\n"), "v1.0.0")))
	findings, count, err := Check(root)
	if err != nil || count != 0 || len(findings) != 1 || !strings.Contains(findings[0].String(), "kit.Spec.Name (field)") {
		t.Fatalf("an unlisted new field fails: %v %d %v", findings, count, err)
	}
	writeFile(t, root, StagedFile, StagedHeader+"kit\tfield\tSpec.Name\n")
	if findings, count, err := Check(root); err != nil || len(findings) != 0 || count != 1 {
		t.Fatalf("a listed field passes: %v %d %v", findings, count, err)
	}
	writeFile(t, root, "kit/kit.go", "package kit\n\ntype Spec struct {\n\t// Name labels the spec in listings.\n\tName string\n}\n\nfunc Define(Spec) {}\n")
	findings, _, err = Check(root)
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].String(), "is listed but now has an acceptable comment") {
		t.Fatalf("a commented field whose line remains fails: %v %v", findings, err)
	}
}
