package docsource

import (
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"strings"
	"testing"
)

func TestRepositoryGuideSources(t *testing.T) {
	testkit.TrackExternalInputs(t)
	documents, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) == 0 {
		t.Fatal("no guide markdown files found")
	}
	if err := Check(documents); err != nil {
		t.Fatal(err)
	}
	if err := CheckTypeScriptInputs("../..", documents); err != nil {
		t.Fatal(err)
	}
}

func TestExpressionDiagnosticTeaching(t *testing.T) {
	data, err := os.ReadFile("../../docs/guides/typed-field-computations.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"expr.predicate[*models.Order]", "expr.selected[*models.Order, int64]", "expr.Predicate[models.Order]", "expr.Selected[models.Order, int64]", "pointer is an internal scope type"} {
		if !strings.Contains(string(data), text) {
			t.Errorf("missing diagnostic explanation %q", text)
		}
	}
	for _, spec := range []struct{ file, alias string }{
		{"expression.go", "type Predicate[S any] = predicate[*S]"},
		{"value.go", "type Selected[S, V any] = selected[*S, V]"},
	} {
		data, err := os.ReadFile("../../database/orm/expr/" + spec.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), spec.alias) {
			t.Errorf("update diagnostic teaching for changed alias %s", spec.alias)
		}
	}
}

func TestHistoricalMigrationLinks(t *testing.T) {
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, label := range []string{"migration guide", "v0.3 migration table", "migration table", "ORM migration guide"} {
		if !strings.Contains(text, "["+label+"](https://github.com/weiloon1234/Foundry-Go/blob/7b364318/docs/guides/orm-migration.md") {
			t.Errorf("historical %s lost its historical target", label)
		}
	}
	data, err = os.ReadFile("../../docs/compatibility.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[datatable guide](guides/datatable.md)") || !strings.Contains(string(data), "[retired-names ledger](../internal/retirement/denied.json)") {
		t.Fatal("compatibility teaching lost its current owners")
	}
}
