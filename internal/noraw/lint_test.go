package noraw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../tests/fixtures/goway")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestResolvedRawSymbols(t *testing.T) {
	testkit.TrackExternalInputs(t)
	dir := fixture(t)
	diagnostics, err := Check(t.Context(), dir, []string{"./testdata/noraw/positive", "./testdata/noraw/negative", "./testdata/noraw/raw.test", "./testdata/noraw/contracts"})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	files, err := filepath.Glob(filepath.Join(dir, "testdata/noraw/*/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(source), "\n") {
			if _, want, ok := strings.Cut(line, "// want "); ok {
				expected[fmt.Sprintf("%s:%d", file, i+1)] = want
			}
		}
	}
	if len(diagnostics) != len(expected) {
		t.Fatalf("got %d, want %d: %v", len(diagnostics), len(expected), diagnostics)
	}
	for _, d := range diagnostics {
		key := fmt.Sprintf("%s:%d", d.Position.Filename, d.Position.Line)
		want := expected[key]
		if want == "" || !strings.HasPrefix(d.Symbol, want) {
			t.Fatal(d)
		}
		if d.String() != key {
			t.Fatalf("diagnostic must contain only file:line: %q", d.String())
		}
		if strings.Contains(d.String(), "private_") {
			t.Fatal("diagnostic disclosed SQL or arguments")
		}
		delete(expected, key)
	}
	if len(expected) > 0 {
		t.Fatal(expected)
	}
}
func TestOrdinaryTypedORMAndLoadFailures(t *testing.T) {
	testkit.TrackExternalInputs(t)
	d, err := Check(t.Context(), fixture(t), []string{"./testdata/noraw/positive"})
	if err != nil || len(d) != 0 {
		t.Fatal(d, err)
	}
	for _, patterns := range [][]string{{"./does_not_exist"}, {"./does_not_exist/..."}, {"-tags=ignore"}, {"./testdata/compilefail/raw_fragment_row_phase"}} {
		if d, err := Check(t.Context(), fixture(t), patterns); err == nil || d != nil {
			t.Fatal("load error passed lint", d, err)
		}
	}
}

func TestFrameworkCallsExcluded(t *testing.T) {
	testkit.TrackExternalInputs(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := Check(t.Context(), root, []string{"./database/orm"})
	if err != nil || len(diagnostics) != 0 {
		t.Fatal(diagnostics, err)
	}
}
func TestStandaloneInterfaceCalls(t *testing.T) {
	testkit.TrackExternalInputs(t)
	diagnostics, err := Check(t.Context(), fixture(t), []string{"./testdata/noraw/contracts"})
	if err != nil || len(diagnostics) != 3 {
		t.Fatal(diagnostics, err)
	}
}
