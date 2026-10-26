package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

type transferTarget struct{ File, Test string }
type transferAudit struct {
	Test         string
	Audited      bool
	Invariant    string
	Disposition  string
	Replacements []transferTarget
}
type transferFile struct {
	Source           string
	SourceSHA256     string          `json:"source_sha256"`
	OldTests         []string        `json:"old_tests"`
	AuditEntries     []transferAudit `json:"audit_entries"`
	Audited          bool
	InvariantSummary string `json:"invariant_summary"`
	Replacements     []transferTarget
}
type transferLedger struct{ Files []transferFile }

// Retirement evidence names assertions, not feature areas. This checks that the
// manually reviewed inventory stays attached to real source and replacement tests.
// 8d3b must archive this source ledger when it deletes the legacy test files.
func validateTransfers(root string, ledger transferLedger) error {
	for _, row := range ledger.Files {
		source, err := os.ReadFile(filepath.Join(root, row.Source))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(source)
		if hex.EncodeToString(sum[:]) != row.SourceSHA256 {
			return fmt.Errorf("%s: stale source hash", row.Source)
		}
		declarations, err := transferTests(row.Source, source)
		if err != nil {
			return err
		}
		names := append([]string(nil), row.OldTests...)
		sort.Strings(names)
		if !reflect.DeepEqual(names, declarations) {
			return fmt.Errorf("%s: incomplete source inventory", row.Source)
		}
		if !row.Audited || row.InvariantSummary == "" || len(row.AuditEntries) != len(names) {
			return fmt.Errorf("%s: incomplete assertion audit", row.Source)
		}
		seen := map[string]bool{}
		for _, audit := range row.AuditEntries {
			if !audit.Audited || audit.Invariant == "" || seen[audit.Test] || !containsTransfer(names, audit.Test) || len(audit.Replacements) == 0 {
				return fmt.Errorf("%s: incomplete audit for %s", row.Source, audit.Test)
			}
			seen[audit.Test] = true
			for _, target := range audit.Replacements {
				if strings.HasPrefix(target.File, "database/query/") || filepath.IsAbs(target.File) || strings.Contains(target.File, "..") {
					return fmt.Errorf("%s: replacement must have a native/shared owner", target.File)
				}
				symbols, err := replacementTests(root, target.File)
				if err != nil {
					return err
				}
				if !containsTransfer(symbols, target.Test) {
					return fmt.Errorf("%s: missing replacement %s", target.File, target.Test)
				}
			}
		}
	}
	return nil
}
func containsTransfer(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
func replacementTests(root, name string) ([]string, error) {
	if !strings.HasSuffix(name, "_test.go") {
		return nil, fmt.Errorf("%s: replacement must be an executable _test.go file", name)
	}
	for _, component := range strings.Split(filepath.ToSlash(name), "/") {
		if component == "testdata" || component == "vendor" || strings.HasPrefix(component, ".") || strings.HasPrefix(component, "_") {
			return nil, fmt.Errorf("%s: replacement is outside compiled test packages", name)
		}
	}
	path := filepath.Join(root, name)
	matches, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, fmt.Errorf("%s: replacement is excluded by the build context", name)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return transferTests(name, source)
}

func transferTests(name string, source []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
	if err != nil {
		return nil, err
	}
	testingImports := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if path != "testing" {
			continue
		}
		alias := "testing"
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		testingImports[alias] = true
	}
	var names []string
	for _, declaration := range file.Decls {
		f, ok := declaration.(*ast.FuncDecl)
		if !ok || f.Recv != nil || f.Body == nil || f.Type.TypeParams.NumFields() != 0 || f.Type.Results.NumFields() != 0 || f.Type.Params.NumFields() != 1 {
			continue
		}
		var want string
		for prefix, parameter := range map[string]string{"Test": "T", "Benchmark": "B", "Fuzz": "F"} {
			if !strings.HasPrefix(f.Name.Name, prefix) {
				continue
			}
			suffix := strings.TrimPrefix(f.Name.Name, prefix)
			r, _ := utf8.DecodeRuneInString(suffix)
			if suffix == "" || !unicode.IsLower(r) {
				want = parameter
			}
		}
		if want == "" {
			continue
		}
		pointer, ok := f.Type.Params.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		switch parameter := pointer.X.(type) {
		case *ast.SelectorExpr:
			qualifier, ok := parameter.X.(*ast.Ident)
			if ok && testingImports[qualifier.Name] && parameter.Sel.Name == want {
				names = append(names, f.Name.Name)
			}
		case *ast.Ident:
			if testingImports["."] && parameter.Name == want {
				names = append(names, f.Name.Name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}
func TestAssertionTransferLedger(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "docs/evidence/goway-stage8d3a-transfers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ledger transferLedger
	if err = json.Unmarshal(data, &ledger); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "database/query/*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Files) != len(files) {
		t.Fatal("legacy source file omitted from ledger", len(ledger.Files), len(files))
	}
	seen := map[string]bool{}
	for _, row := range ledger.Files {
		if seen[row.Source] {
			t.Fatal("duplicate source", row.Source)
		}
		seen[row.Source] = true
	}
	for _, file := range files {
		relative, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		if !seen[filepath.ToSlash(relative)] {
			t.Fatal("missing source", relative)
		}
	}
	if err = validateTransfers(root, ledger); err != nil {
		t.Fatal(err)
	}
}
func TestAssertionTransferLedgerRejectsNominalMappings(t *testing.T) {
	root := t.TempDir()
	source := []byte("package p\nimport \"testing\"\nfunc TestOld(t *testing.T){}\n")
	replacement := []byte("package p\nimport \"testing\"\nfunc TestNative(t *testing.T){}\n")
	if err := os.WriteFile(filepath.Join(root, "old_test.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "native_test.go"), replacement, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	valid := func() transferLedger {
		return transferLedger{Files: []transferFile{{Source: "old_test.go", SourceSHA256: hex.EncodeToString(hash[:]), OldTests: []string{"TestOld"}, Audited: true, InvariantSummary: "Exact assertion", AuditEntries: []transferAudit{{Test: "TestOld", Audited: true, Invariant: "Exact assertion", Replacements: []transferTarget{{File: "native_test.go", Test: "TestNative"}}}}}}}
	}
	if err := validateTransfers(root, valid()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*transferFile){
		"legacy replacement":  func(r *transferFile) { r.AuditEntries[0].Replacements[0].File = "database/query/native_test.go" },
		"missing audit":       func(r *transferFile) { r.AuditEntries = nil },
		"unaudited entry":     func(r *transferFile) { r.AuditEntries[0].Audited = false },
		"missing invariant":   func(r *transferFile) { r.AuditEntries[0].Invariant = "" },
		"missing replacement": func(r *transferFile) { r.AuditEntries[0].Replacements[0].Test = "TestFeatureArea" },
		"stale source":        func(r *transferFile) { r.SourceSHA256 = "stale" },
		"omitted source test": func(r *transferFile) { r.OldTests = nil },
	} {
		t.Run(name, func(t *testing.T) {
			ledger := valid()
			mutate(&ledger.Files[0])
			if err := validateTransfers(root, ledger); err == nil {
				t.Fatal("nominal mapping accepted")
			}
		})
	}
}

func TestAssertionTransferLedgerRejectsNonExecutableReplacements(t *testing.T) {
	for _, test := range []struct{ name, file, source, symbol string }{
		{"archived", "native_test.go.txt", `package p; import "testing"; func TestNative(t *testing.T){}`, "TestNative"},
		{"build ignored", "native_test.go", "//go:build ignore\n\npackage p\nimport \"testing\"\nfunc TestNative(t *testing.T){}", "TestNative"},
		{"testdata", "testdata/native_test.go", `package p; import "testing"; func TestNative(t *testing.T){}`, "TestNative"},
		{"vendor", "vendor/example/native_test.go", `package p; import "testing"; func TestNative(t *testing.T){}`, "TestNative"},
		{"prefix helper", "native_test.go", `package p; import "testing"; func Testhelper(t *testing.T){}`, "Testhelper"},
		{"no parameter", "native_test.go", `package p; func TestNative(){}`, "TestNative"},
		{"wrong parameter", "native_test.go", `package p; import "testing"; func TestNative(b *testing.B){}`, "TestNative"},
		{"fake testing import", "native_test.go", `package p; import testing "other/testing"; func TestNative(t *testing.T){}`, "TestNative"},
		{"result", "native_test.go", `package p; import "testing"; func TestNative(t *testing.T) error{return nil}`, "TestNative"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := []byte("package p\nimport \"testing\"\nfunc TestOld(t *testing.T){}\n")
			if err := os.WriteFile(filepath.Join(root, "old_test.go"), source, 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, test.file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(source)
			ledger := transferLedger{Files: []transferFile{{Source: "old_test.go", SourceSHA256: hex.EncodeToString(hash[:]), OldTests: []string{"TestOld"}, Audited: true, InvariantSummary: "Executable assertion", AuditEntries: []transferAudit{{Test: "TestOld", Audited: true, Invariant: "Executable assertion", Replacements: []transferTarget{{File: test.file, Test: test.symbol}}}}}}}
			if err := validateTransfers(root, ledger); err == nil {
				t.Fatal("non-executable replacement accepted")
			}
		})
	}
}
func TestTransferEntryPointSignatures(t *testing.T) {
	source := []byte(`package p
 import check "testing"
 func TestNative(t *check.T){}
 func BenchmarkNative(b *check.B){}
 func FuzzNative(f *check.F){}
 func BenchmarkWrong(t *check.T){}
 func FuzzWrong(t *check.T){}
 func TestGeneric[T any](t *check.T){}
 func TestValue(t check.T){}
 func TestMultiple(t,u *check.T){}
 `)
	got, err := transferTests("native_test.go", source)
	if err != nil || !reflect.DeepEqual(got, []string{"BenchmarkNative", "FuzzNative", "TestNative"}) {
		t.Fatal(got, err)
	}
}
