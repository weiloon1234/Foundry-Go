package datatable

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestTableMetadataReusesDTOAndOwnsRegistrationSlices(t *testing.T) {
	spec := reportSpec()
	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	before, err := table.Description()
	if err != nil {
		t.Fatal(err)
	}
	if before.ID != "reports.members" || len(before.Columns) != 4 || !before.Columns[0].Value.Quoted || before.Columns[1].SearchOperator != InsensitiveContains || !before.Columns[2].Value.Value.Nullable || before.Columns[3].Value.Value.Format != contract.DecimalFormat {
		t.Fatal("typed declaration metadata lost semantics")
	}
	spec.Columns[0] = ColumnRegistration[reportRecord, ReportRow]{}
	spec.DefaultSort[0].Column = "missing"
	spec.Stable[0] = nil
	copy, _ := table.Description()
	copy.Columns[1].Filter.Operators[0] = "mutated"
	copy.Row.Types[0].ID = "mutated"
	copy.DefaultSort[0].Column = "mutated"
	after, _ := table.Description()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("table retained mutable declaration or metadata buffers")
	}
	registry, err := NewRegistry(table.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(table.Registration(), table.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate table identity accepted", err)
	}
	if _, err := NewRegistry(Registration{}); err == nil {
		t.Fatal("undefined table accepted")
	}
	descriptions, err := registry.Descriptions()
	if err != nil || len(descriptions) != 1 || !reflect.DeepEqual(descriptions[0], before) {
		t.Fatal("manifest contribution differs from runtime", err)
	}
}

func TestTableRejectsMissingAuthorityAndConflictingDeclarations(t *testing.T) {
	for name, change := range map[string]func(*Spec[reportRecord, ReportRow, reportActor]){
		"invalid ID":       func(s *Spec[reportRecord, ReportRow, reportActor]) { s.ID = "reports/users" },
		"no authorization": func(s *Spec[reportRecord, ReportRow, reportActor]) { s.Authorize = nil },
		"no scope":         func(s *Spec[reportRecord, ReportRow, reportActor]) { s.Source = nil },
		"no unique order":  func(s *Spec[reportRecord, ReportRow, reportActor]) { s.Stable = nil },
		"nil order": func(s *Spec[reportRecord, ReportRow, reportActor]) {
			s.Stable = []query.ProjectionOrder[reportRecord]{nil}
		},
		"no columns":       func(s *Spec[reportRecord, ReportRow, reportActor]) { s.Columns = nil },
		"duplicate column": func(s *Spec[reportRecord, ReportRow, reportActor]) { s.Columns = append(s.Columns, s.Columns[0]) },
		"undefined column": func(s *Spec[reportRecord, ReportRow, reportActor]) {
			s.Columns[0] = ColumnRegistration[reportRecord, ReportRow]{}
		},
		"unknown sort": func(s *Spec[reportRecord, ReportRow, reportActor]) { s.DefaultSort[0].Column = "not_a_column" },
		"wrong scalar metadata": func(s *Spec[reportRecord, ReportRow, reportActor]) {
			_, name, _, _ := reportFields()
			s.Columns[0] = DefineColumn[reportRecord](validation.DefineField("id", func(ReportRow) string { return "forged" }), "reports.id").FilterBy(Where(foundryhttp.StringQuery[string](), name)).Registration()
		},
		"no exportable column": func(s *Spec[reportRecord, ReportRow, reportActor]) {
			s.Columns = []ColumnRegistration[reportRecord, ReportRow]{DefineColumn[reportRecord](validation.DefineField("id", func(row ReportRow) int64 { return row.ID }), "reports.id").Registration()}
			s.DefaultSort = nil
		},
		"overlapping extra filter": func(s *Spec[reportRecord, ReportRow, reportActor]) {
			id, _, _, _ := reportFields()
			s.Filters = []FilterRegistration[reportRecord]{DefineFilter("id", "reports.id", Where(foundryhttp.IntegerQuery[int64](), id))}
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := reportSpec()
			change(&spec)
			table := Define(spec)
			if table.Validate() == nil {
				t.Fatal("invalid table accepted")
			}
			if info, err := table.Description(); err == nil || !reflect.DeepEqual(info, Description{}) {
				t.Fatal("invalid table exposed partial metadata")
			}
		})
	}
}

// Rows are output: one reaching a password hint fails its declaration, whether
// or not clients are exported. Password hints are input-only.
func TestTableRowsRejectPasswordPresentation(t *testing.T) {
	spec := reportSpec()
	schema, err := spec.Row.Description()
	if err != nil {
		t.Fatal(err)
	}
	for i, typ := range schema.Types {
		for j, property := range typ.Properties {
			if typ.ID == schema.Root && property.Name == "note" {
				schema.Types[i].Properties[j].Presentation = contract.Presentation{Kind: contract.PasswordPresentation}
			}
		}
	}
	spec.Row = contract.DefineJSON[ReportRow](schema)
	if err := spec.Row.Validate(); err != nil {
		t.Fatal(err)
	}
	table := Define(spec)
	if err := table.Validate(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "password") {
		t.Fatal("password row accepted", err)
	}
	if _, err := NewRegistry(table.Registration()); err == nil {
		t.Fatal("password row registered")
	}
}

func TestManagerRequiresExactRegisteredDeclaration(t *testing.T) {
	first := Define(reportSpec())
	second := Define(reportSpec())
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Dependencies{Database: new(database.DB), Locales: locales, Labels: func(context.Context, i18n.LocaleID, i18n.MessageKey) (string, error) { return "Label", nil }}, DefaultConfig(), first.Registration())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	if err := first.check(manager); err != nil {
		t.Fatal(err)
	}
	if err := second.check(manager); !errors.Is(err, fault.Missing) {
		t.Fatal("same name substituted a different authority declaration", err)
	}
	if _, err := first.Inspect(t.Context(), manager, reportActor{}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("inspection skipped authority", err)
	}
	if info, err := first.Inspect(t.Context(), manager, reportActor{Allowed: true}); err != nil || info.ID != first.ID() {
		t.Fatal("authorized inspection failed", err)
	}
}

func TestSearchableColumnsCannotSpanWhereAndHavingPhases(t *testing.T) {
	spec := reportSpec()
	// No built-in aggregate offers text search, so shape the declaration
	// directly: the rule applies to whichever typed sources a table combines.
	grouped := spec.Columns[3].declaration
	filter := *grouped.filter
	filter.info.Phase = HavingPhase
	filter.info.Operators = append(filter.info.Operators, Contains)
	grouped.filter, grouped.searchable = &filter, true
	spec.Columns[3] = ColumnRegistration[reportRecord, ReportRow]{declaration: grouped}
	if err := Define(spec).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("mixed-phase global search accepted at declaration", err)
	}
	grouped.searchable = false
	spec.Columns[3] = ColumnRegistration[reportRecord, ReportRow]{declaration: grouped}
	if err := Define(spec).Validate(); err != nil {
		t.Fatal("single-phase search rejected", err)
	}
}
