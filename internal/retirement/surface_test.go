package retirement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
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
	got := findingLines(missing)
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
	if err != nil || len(missing) != 1 || !strings.Contains(missing[0].String(), "pkg.Set (type)") {
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
		t.Fatalf("%s", findingLines(lost))
	}
	lost := LostEntries([]byte("pkg\tA\npkg\tB\n"), map[string][]retiredDeclaration{"pkg": {{Name: "A", NoReplacement: true}}})
	if len(lost) != 1 || !strings.Contains(lost[0].String(), "pkg.B was retired in the previous release and is missing from the ledger; restore the entry") {
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
	writeSource(t, root, "tools/gen/gen.go", "package gen\n\nfunc Tool() {}\n")
	writeSource(t, root, "cmd/lib/lib.go", "package lib\n\nfunc Command() {}\n")
	writeSource(t, root, "a/tools/tools.go", "package tools\n\nfunc Public() {}\n")
	writeSource(t, root, "a/tests/tests.go", "package tests\n\nfunc Public() {}\n")
	writeSource(t, root, "a/cmd/cmd.go", "package cmd\n\nfunc Public() {}\n")
	writeSource(t, root, "other/testdata/t/t.go", "package t\n\nfunc Data() {}\n")
	writeSource(t, root, "nested/go.mod", "module nested\n")
	writeSource(t, root, "nested/n.go", "package nested\n\nfunc Nested() {}\n")
	data, err := Surface(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimPrefix(string(data), surfaceHeader)
	want := strings.Join([]string{
		"a/cmd\tfunc\tPublic",
		"a/tests\tfunc\tPublic",
		"a/tools\tfunc\tPublic",
		"shape\talias\tAlias",
		"shape\tconst\tLimit",
		"shape\tfield\tAlias.Embedded",
		"shape\tfield\tAlias.Size",
		"shape\tfield\tBox.Embedded",
		"shape\tfield\tBox.Size",
		"shape\tfield\tEmbedded.Inner",
		"shape\tfunc\tLinuxOnly",
		"shape\tfunc\tMap",
		"shape\tfunc\tNew",
		"shape\tfunc\tWindowsOnly",
		"shape\tmethod\tAlias.Value",
		"shape\tmethod\tBox.Value",
		"shape\tmethod\tReader.Read",
		"shape\tmethod*\tAlias.Pointer",
		"shape\tmethod*\tBox.Pointer",
		"shape\ttype\tBox",
		"shape\ttype\tEmbedded",
		"shape\ttype\tReader",
		"shape\tvar\tDefault",
		"shape/uses\talias\tWrapped",
		"shape/uses\tfield\tWrapped.Field",
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

func TestKindChangeIsReported(t *testing.T) {
	previous := surfaceOf("pkg\tfunc\tLimit", "pkg\ttype\tBox", "pkg\tmethod\tBox.Open", "pkg\ttype\tOld", "pkg\tvar\tShared")
	current := surfaceOf("pkg\tconst\tLimit", "pkg\ttype\tBox", "pkg\tmethod*\tBox.Open", "pkg\talias\tOld", "pkg\tvar\tShared")
	missing, err := RemovedWithoutEntry(previous, current, nil)
	if err != nil || len(missing) != 1 || !strings.Contains(missing[0].String(), "pkg.Limit changed from func to const since the previous release") {
		t.Fatalf("only the func to const change is a break (a pointer receiver or an alias is not): %v %v", err, missing)
	}
	missing, err = RemovedWithoutEntry(previous, current, ledgerOf(retiredDeclaration{Name: "Limit", NoReplacement: true}))
	if err != nil || len(missing) != 0 {
		t.Fatalf("a ledger entry covers the kind change: %v %v", err, missing)
	}
}

func TestTypeBecomingAliasKeepsItsMembers(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n\ngo 1.22\n")
	writeSource(t, root, "kit/kit.go", "package kit\n\ntype Box struct{ Size int }\n\nfunc (Box) Open() {}\n")
	before, err := Surface(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "kit/kit.go", "package kit\n\ntype Core struct{ Size int }\n\nfunc (Core) Open() {}\n\ntype Box = Core\n")
	after, err := Surface(root)
	if err != nil {
		t.Fatal(err)
	}
	if missing, err := RemovedWithoutEntry(before, after, nil); err != nil || len(missing) != 0 {
		t.Fatalf("an alias has the members of its target, so none is removed: %v %v", err, missing)
	}
	writeSource(t, root, "kit/kit.go", "package kit\n\ntype Core struct{}\n\ntype Box = Core\n")
	after, err = Surface(root)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := RemovedWithoutEntry(before, after, nil)
	if err != nil || len(missing) != 2 || !strings.Contains(findingLines(missing), "kit.Box.Open (method)") || !strings.Contains(findingLines(missing), "kit.Box.Size (field)") {
		t.Fatalf("members the target lost are removed: %v %v", err, missing)
	}
}

func TestSurfaceLoadFailureIsLocated(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n\ngo 1.22\n")
	writeSource(t, root, "kit/kit.go", "package kit\n\nfunc Keep() {}\n\nvar Broken Missing\n")
	_, err := Surface(root)
	if err == nil || !strings.Contains(err.Error(), "kit/kit.go:5: type-check for linux/amd64 failed: undefined: Missing") {
		t.Fatalf("a type error must fail with its location, not yield a partial inventory: %v", err)
	}

	root = t.TempDir()
	writeSource(t, root, "go.mod", "module "+strings.TrimSuffix(framework, "/")+"\n\ngo 1.22\n")
	writeSource(t, root, "kit/kit.go", "package kit\n\nfunc Keep() {}\n")
	writeSource(t, root, "kit/native.go", "package kit\n\n// #include <stdio.h>\nimport \"C\"\n\nfunc Native() {}\n")
	_, err = Surface(root)
	if err == nil || !strings.Contains(err.Error(), "kit/native.go:6: a cgo file declares exported API that the inventory cannot record") {
		t.Fatalf("exported API in a cgo file must fail, not vanish: %v", err)
	}
	writeSource(t, root, "kit/native.go", "package kit\n\n// #include <stdio.h>\nimport \"C\"\n\nfunc native() {}\n")
	if _, err := Surface(root); err != nil {
		t.Fatalf("a cgo file without exported API is not a failure: %v", err)
	}
}

// Cutting a release without replacing the baseline would let a name added and
// removed within one release need an entry; the ordinary tests catch the step.
func TestBaselineIsTheNewestReleasedVersion(t *testing.T) {
	baseline, err := os.ReadFile(filepath.Join("../..", ReleaseSurfaceFile))
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile(filepath.Join("../..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBaselineVersion(baseline, changelog); err != nil {
		t.Fatal(err)
	}
}

// The release commit renames the changelog heading and replaces the baseline
// together, so the tagged tree is the only state where the two agree on the new
// version; each half alone is refused.
func TestBaselineVersionAgreesWithTheChangelogHeading(t *testing.T) {
	baseline := func(version string) []byte { return []byte("# Recorded at release " + version + ".\nrest\n") }
	changelog := func(headings ...string) []byte {
		return []byte("# Changelog\n\n## Unreleased\n\n- change\n\n" + strings.Join(headings, "\n\n- text\n\n") + "\n")
	}
	for name, tc := range map[string]struct {
		baseline  []byte
		changelog []byte
		want      string
	}{
		"release commit":                {baseline("v0.53.0"), changelog("## v0.53.0 (2026-12-01)", "## v0.52.0"), ""},
		"between releases":              {baseline("v0.52.0"), changelog("## v0.52.0"), ""},
		"heading without the baseline":  {baseline("v0.52.0"), changelog("## v0.53.0", "## v0.52.0"), "records v0.52.0 but CHANGELOG.md's newest released version is v0.53.0"},
		"baseline without the heading":  {baseline("v0.53.0"), changelog("## v0.52.0"), "records v0.53.0 but CHANGELOG.md's newest released version is v0.52.0"},
		"baseline without a version":    {[]byte("pkg\tfunc\tA\n"), changelog("## v0.52.0"), "must start with the line"},
		"changelog without any release": {baseline("v0.52.0"), changelog(), "has no released version heading"},
	} {
		err := CheckBaselineVersion(tc.baseline, tc.changelog)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
	if err := CheckBaselineVersion(baseline("v0.52.0"), changelog("## v0.53.0", "## v0.52.0")); err == nil || !strings.Contains(err.Error(), "run go run ./internal/cmd/exportsurface --release v0.53.0 in the release commit, with the heading") {
		t.Errorf("the message names the release step: %v", err)
	}
}

func TestReleaseTagForm(t *testing.T) {
	for tag, want := range map[string]bool{"v0.53.0": true, "v1.20.3": true, "0.53.0": false, "v0.53": false, "v0.53.0-rc1": false, "v01.2.3": false, "v0.53.0\n# x": false, "": false} {
		if ValidReleaseTag(tag) != want {
			t.Errorf("ValidReleaseTag(%q) = %v, want %v", tag, !want, want)
		}
	}
	if version, ok := ReleaseVersion([]byte("# Recorded at release v0.52.0.\nrest")); !ok || version != "v0.52.0" {
		t.Fatalf("version of a baseline: %q %v", version, ok)
	}
}

func findingLines(findings []diagnostic.Diagnostic) string {
	lines := make([]string, len(findings))
	for i, finding := range findings {
		lines[i] = finding.String()
	}
	return strings.Join(lines, "\n")
}

// Every finding of the ledger lane is a file the developer edits, so each has the
// shared shape: a rule, the retired-names teaching reference, no printed zero.
func TestLedgerFindingsUseTheSharedShape(t *testing.T) {
	removed, err := RemovedWithoutEntry(surfaceOf("pkg\tfunc\tGone", "pkg\tfunc\tLimit", "old\tfunc\tOne"), surfaceOf("pkg\tconst\tLimit"), nil)
	if err != nil || len(removed) != 3 {
		t.Fatalf("removed, changed kind and removed package: %v %v", err, removed)
	}
	lost := LostEntries([]byte("pkg\tB\n"), nil)
	_, entry := parseLedger([]byte(`{"pkg":[{"name":"A"}]}`), LedgerFile)
	_, decode := parseLedger([]byte(`[]`), LedgerFile)
	findings := append(removed, lost...)
	for _, err := range []error{entry, decode, StaleSurface().Err()} {
		found, other := diagnostic.Findings(err)
		if len(found) != 1 || len(other) != 0 {
			t.Fatalf("one finding expected: %v %v", found, other)
		}
		findings = append(findings, found...)
	}
	wantRules := map[string]bool{"removal-unledgered": false, "ledger-entry-lost": false, "ledger-shape": false, "surface-stale": false}
	for _, finding := range findings {
		text := finding.String()
		if _, known := wantRules[finding.Rule.ID]; !known {
			t.Errorf("unexpected rule in %s", text)
		}
		wantRules[finding.Rule.ID] = true
		if !strings.Contains(text, "["+finding.Rule.ID+"] ") || !strings.HasSuffix(text, ". Foundry guide: developer-tooling-and-testing#retired-names") || strings.Contains(text, ":0") {
			t.Errorf("not the shared shape: %s", text)
		}
		if !strings.HasPrefix(text, "internal/retirement/") {
			t.Errorf("a finding names its file: %s", text)
		}
	}
	for rule, seen := range wantRules {
		if !seen {
			t.Errorf("rule %s was never printed", rule)
		}
	}
}
