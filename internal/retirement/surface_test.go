package retirement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func surfaceOf(lines ...string) []byte {
	return []byte(surfaceHeader + strings.Join(lines, "\n") + "\n")
}

func ledgerOf(entries ...retiredDeclaration) map[string][]retiredDeclaration {
	return map[string][]retiredDeclaration{"pkg": entries}
}

func TestRemovedIdentifierNeedsLedgerEntry(t *testing.T) {
	previous := surfaceOf(
		"pkg\tfunc\tKept",
		"pkg\tfunc\tGone",
		"pkg\ttype\tBox",
		"pkg\tmethod\tBox.Open",
		"pkg\tmethod*\tBox.Close",
		"pkg\tfield\tBox.Size",
		"pkg\ttype\tSet",
		"pkg\tmethod\tSet.Add",
		"pkg\tconst\tLimit",
		"pkg\talias\tOld",
		"oldpkg\tfunc\tOne",
		"oldpkg\tfunc\tTwo",
	)
	current := surfaceOf(
		"pkg\tfunc\tKept",
		"pkg\ttype\tBox",
		"pkg\talias\tOld",
		"newpkg\tfunc\tOne",
	)
	missing, err := RemovedWithoutEntry(previous, current, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(missing, "\n")
	for _, want := range []string{
		"pkg.Gone (func) was removed since the previous release; add it to internal/retirement/denied.json under \"pkg\"",
		"pkg.Box.Open (method)",
		"pkg.Box.Close (method*)",
		"pkg.Box.Size (field)",
		"pkg.Set (type)",
		"pkg.Limit (const)",
		"package oldpkg was removed since the previous release; add {\"name\": \"package\"}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing report %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"pkg.Kept", "pkg.Old", "Set.Add", "oldpkg.One", "oldpkg.Two", "newpkg", "pkg.Box "} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected report %q in:\n%s", unwanted, got)
		}
	}
	if len(missing) != 7 {
		t.Errorf("got %d reports, want 7:\n%s", len(missing), got)
	}

	// Each kind of ledger entry satisfies exactly the identifier it names.
	ledger := map[string][]retiredDeclaration{
		"pkg": {
			{Name: "Gone", Replacement: "pkg.Kept"},
			{Name: "Box.Open", NoReplacement: true},
			{Name: "Box.Close", NoReplacement: true},
			{Name: "Box.Size", NoReplacement: true},
			{Name: "Set", NoReplacement: true},
			{Name: "Limit", NoReplacement: true},
		},
		"oldpkg": {{Name: PackageEntry, Replacement: "newpkg"}},
	}
	missing, err = RemovedWithoutEntry(previous, current, ledger)
	if err != nil || len(missing) != 0 {
		t.Fatalf("complete ledger refused: %v %v", err, missing)
	}
	delete(ledger, "oldpkg")
	ledger["pkg"] = ledger["pkg"][1:]
	if missing, _ = RemovedWithoutEntry(previous, current, ledger); len(missing) != 2 {
		t.Fatalf("a removed package and a removed function must both be reported again: %v", missing)
	}
}

func TestRemovedMembersOfARemovedTypeFollowTheTypeEntry(t *testing.T) {
	previous := surfaceOf("pkg\ttype\tSet", "pkg\tmethod\tSet.Add", "pkg\tfield\tSet.Name")
	current := surfaceOf("pkg\tfunc\tOther")
	missing, err := RemovedWithoutEntry(previous, current, nil)
	if err != nil || len(missing) != 1 || !strings.Contains(missing[0], "pkg.Set (type)") {
		t.Fatalf("only the removed type is reported: %v %v", err, missing)
	}
	missing, err = RemovedWithoutEntry(previous, current, ledgerOf(retiredDeclaration{Name: "Set", Replacement: "pkg.Other"}))
	if err != nil || len(missing) != 0 {
		t.Fatalf("a type entry covers its members: %v %v", err, missing)
	}
}

func TestLedgerEntryNamesReplacementOrExplicitNone(t *testing.T) {
	for name, tc := range map[string]struct {
		json string
		want string
	}{
		"replacement":  {`{"pkg":[{"name":"A","replacement":"pkg.B"}]}`, ""},
		"none":         {`{"pkg":[{"name":"A","noReplacement":true}]}`, ""},
		"neither":      {`{"pkg":[{"name":"A"}]}`, "pkg.A must have exactly one of replacement and noReplacement: true"},
		"both":         {`{"pkg":[{"name":"A","replacement":"pkg.B","noReplacement":true}]}`, "pkg.A must have exactly one of replacement and noReplacement: true"},
		"false":        {`{"pkg":[{"name":"A","noReplacement":false}]}`, "pkg.A must have exactly one of replacement and noReplacement: true"},
		"misspelling":  {`{"pkg":[{"name":"A","noReplacment":true}]}`, "unknown field"},
		"bare string":  {`{"pkg":["A"]}`, "cannot unmarshal"},
		"not an array": {`{"pkg":"A"}`, "cannot unmarshal"},
	} {
		_, err := parseLedger([]byte(tc.json), "denied.json")
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

func TestLedgerEntriesCannotDisappear(t *testing.T) {
	current, err := retiredDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(filepath.Join("../..", ReleaseLedgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(previous) == 0 {
		t.Fatal("the previous release's retired names are empty")
	}
	if lost := LostEntries(previous, current); len(lost) != 0 {
		t.Fatalf("%s", strings.Join(lost, "\n"))
	}
	lost := LostEntries([]byte("pkg\tA\npkg\tB\n"), map[string][]retiredDeclaration{"pkg": {{Name: "A", NoReplacement: true}}})
	if len(lost) != 1 || !strings.Contains(lost[0], "pkg.B was retired in the previous release and is missing from the ledger; restore the entry") {
		t.Fatalf("a dropped entry must be named: %v", lost)
	}
}

func TestReleaseNamesRecordTheLedger(t *testing.T) {
	entries := map[string][]retiredDeclaration{"b": {{Name: "Y", NoReplacement: true}, {Name: "X", Replacement: "b.Z"}}, "a": {{Name: "M", NoReplacement: true}}}
	if got := string(RetiredNames(entries)); got != "a\tM\nb\tX\nb\tY\n" {
		t.Fatalf("sorted names of every package: %q", got)
	}
}

// The recorded inventories are fresh (make generate-check); this test makes the
// completeness rule part of the ordinary suite.
func TestRecordedSurfaceHasNoUnledgeredRemoval(t *testing.T) {
	if err := CheckRemovals("../.."); err != nil {
		t.Fatal(err)
	}
}

func TestSurfaceRecordsExportedAPI(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n\ngo 1.22\n")
	writeSource(t, root, "shape/shape.go", `package shape

import "strings"

const Limit = 3
const hidden = 1

var Default = strings.ToUpper("x")

type Box struct {
	Size int
	name string
	Embedded
}

type Embedded struct{ Inner int }

func (Box) Value() int     { return 0 }
func (*Box) Pointer()      {}
func (Box) private()       {}
func New() Box             { return Box{} }
func Map[T any](v T) T     { return v }

type Reader interface {
	Read() int
	write()
}

type Alias = Box
type hiddenType struct{ Visible int }

func (hiddenType) Method() {}
`)
	writeSource(t, root, "shape/only_linux.go", "package shape\n\nfunc LinuxOnly() {}\n")
	writeSource(t, root, "shape/only_windows.go", "package shape\n\nfunc WindowsOnly() {}\n")
	writeSource(t, root, "shape/shape_test.go", "package shape\n\nfunc TestOnly() {}\n")
	writeSource(t, root, "shape/internal/inner/inner.go", "package inner\n\nfunc Inner() {}\n")
	writeSource(t, root, "shape/uses/uses.go", "package uses\n\nimport \""+framework+"shape/internal/inner\"\n\ntype Wrapped = inner.T\n")
	writeSource(t, root, "shape/internal/inner/t.go", "package inner\n\ntype T struct{ Field int }\n")
	writeSource(t, root, "cmd/tool/main.go", "package main\n\nfunc Exported() {}\n\nfunc main() {}\n")
	writeSource(t, root, "tests/fixtures/x/x.go", "package x\n\nfunc Fixture() {}\n")
	writeSource(t, root, "other/testdata/t/t.go", "package t\n\nfunc Data() {}\n")
	writeSource(t, root, "nested/go.mod", "module nested\n")
	writeSource(t, root, "nested/n.go", "package nested\n\nfunc Nested() {}\n")
	data, err := Surface(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimPrefix(string(data), surfaceHeader)
	want := strings.Join([]string{
		"shape\talias\tAlias",
		"shape\tconst\tLimit",
		"shape\tfield\tBox.Embedded",
		"shape\tfield\tBox.Size",
		"shape\tfield\tEmbedded.Inner",
		"shape\tfunc\tLinuxOnly",
		"shape\tfunc\tMap",
		"shape\tfunc\tNew",
		"shape\tfunc\tWindowsOnly",
		"shape\tmethod\tBox.Value",
		"shape\tmethod\tReader.Read",
		"shape\tmethod*\tBox.Pointer",
		"shape\ttype\tBox",
		"shape\ttype\tEmbedded",
		"shape\ttype\tReader",
		"shape\tvar\tDefault",
		"shape/uses\talias\tWrapped",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("inventory\n%s\nwant\n%s", got, want)
	}
	if again, err := Surface(root); err != nil || string(again) != string(data) {
		t.Fatalf("the inventory must be deterministic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, SurfaceFile)); err == nil {
		t.Fatal("computing the inventory must not write it")
	}
}
