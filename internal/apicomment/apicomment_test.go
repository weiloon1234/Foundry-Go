package apicomment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	Hook func(Event)
	// Result is not a caller input here but is reached through Spec.
	Nested *Nested
}

type Options struct {
	Mode string // Mode selects how the spec runs.
}

type Event struct{ Detail string }

type Nested struct{ Depth int }

// Define declares a spec.
func Define(spec Spec) {}

// Output only appears in results.
type Output struct{ Value string }

// Run returns an output.
func Run() Output { return Output{} }
`)
	want := map[string]string{
		"field Spec.Name":    "",
		"field Spec.Size":    problemMissing,
		"field Spec.Inner":   problemMissing,
		"field Spec.Hook":    problemMissing,
		"field Spec.Nested":  problemName,
		"field Options.Mode": "",
		"field Event.Detail": problemMissing,
		"field Nested.Depth": problemMissing,
	}
	for key, problem := range want {
		if got[key] != problem {
			t.Errorf("%s: got %q, want %q", key, got[key], problem)
		}
	}
	if _, present := got["field Output.Value"]; present {
		t.Error("a struct that only results expose is not required")
	}
	if len(got) != len(want) {
		t.Errorf("declarations: %v", got)
	}
}

func TestCallerInputsAreFoundThroughTypeShapes(t *testing.T) {
	got := analyse(t, `package kit

type InSlice struct{ A int }
type InMap struct{ B int }
type InPointer struct{ C int }
type InChan struct{ D int }
type InArray struct{ E int }
type InGeneric struct{ F int }
type InMethod struct{ G int }
type InInterface struct{ H int }
type Embedded struct{ I int }
type Alias = AliasTarget
type AliasTarget struct{ J int }
type InCallbackField struct{ K int }
type Box[T any] struct{ Item T }
type Holder struct{ Embedded }
type Service struct{}
type Iface interface{ Take(InInterface) }
type Widget struct{ Hook func(InCallbackField) }

func Slices(v []InSlice, w map[string]InMap, x *InPointer, y chan InChan, z [2]InArray) {}
func Generic(b Box[InGeneric]) {}
func Embeds(h Holder) {}
func Aliased(a Alias) {}
func (Service) Do(m InMethod) {}
`)
	for _, key := range []string{"field InSlice.A", "field InMap.B", "field InPointer.C", "field InChan.D", "field InArray.E", "field InGeneric.F", "field InMethod.G", "field InInterface.H", "field Embedded.I", "field Holder.Embedded", "field AliasTarget.J", "field InCallbackField.K", "field Box.Item"} {
		if _, ok := got[key]; !ok {
			t.Errorf("%s is a caller input and was not required; have %v", key, got)
		}
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
		name, text, want string
	}{
		{"Timeout", "Timeout is the timeout.", problemRestating},
		{"RetryLimit", "RetryLimit is the retry limit", problemRestating},
		{"Name", "Name.", problemRestating},
		{"Timeout", "Timeout is how long a request may run.", ""},
		{"ID", "ID identifies the route.", ""},
		{"Timeout", "The timeout for requests.", problemName},
		{"Timeouts", "Timeout lists them.", problemName},
		{"Max", "Maximum rows.", problemName},
	} {
		if got := judgeText(tc.text, tc.name); got != tc.want {
			t.Errorf("%q for %s: got %q, want %q", tc.text, tc.name, got, tc.want)
		}
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
	staged := ParseStaged([]byte(StagedHeader + "kit\tfield\tSpec.Fixed\nkit\tfield\tSpec.Gone\nkit\tfield\tSpec.Listed\n"))
	findings := Compare(declarations, staged)
	var text []string
	for _, f := range findings {
		text = append(text, f.String())
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{
		"kit/kit.go:3: [api-comment] kit.Spec.New (field): has no doc comment; add a comment that starts with \"New\"",
		"internal/apicomment/uncommented.txt:6: [api-comment] kit field Spec.Fixed: is listed but now has an acceptable comment; remove the line",
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
	if len(Compare(declarations[:1], ParseStaged([]byte(after)))) != 0 {
		t.Fatal("the shrunk file must pass")
	}
	if again := string(Shrink(declarations, []byte(after))); again != after {
		t.Fatalf("shrinking is idempotent: %q", again)
	}
}

func TestCheckEndToEnd(t *testing.T) {
	root := newRoot(t, "package kit\n\ntype Spec struct{ Name string }\n\nfunc Define(Spec) {}\n")
	writeFile(t, root, StagedFile, StagedHeader)
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
