package datatable

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestUnionTextCollationSkipsMarkedResult(t *testing.T) {
	type result struct {
		Status value.Nullable[enumSortStatus]
	}
	type fields struct {
		rows.Row[result]
		Status rows.Field[result, value.Nullable[enumSortStatus]]
	}
	schema := rows.Define(func(b rows.Builder[result], r *result) fields {
		return fields{b.Row(), rows.NullableTextOf(b, &r.Status)}
	})
	source := rows.From(enumSortModels.Query())
	plain := rows.RecordOf(rows.Select(source, schema, rows.MapSelected(schema.Status, expr.RawWithCodec[enumSortRecord](codec.Nullable(codec.String[enumSortStatus]()), "NULL"))))
	marked := rows.RecordOf(rows.Select(source, schema, rows.Map(schema.Status, expr.Value(orm.NullableEnumExpr(enumSortModels.OptStatus)))))
	for _, set := range []rows.SetQuery[result]{rows.Union(plain, marked), rows.Union(marked, plain)} {
		selected := expr.AsSelected(rows.At(set.Scope(), schema.Status))
		query := rows.SelectScalar(set, selected)
		if _, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(query, expr.AscValue(expr.CollateNullableValue(selected, expr.CollationC))))); !errors.Is(err, fault.Invalid) {
			t.Fatal("UNION accepted explicit collation", err)
		}
		sort := SortBy(selected)
		baseline, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(query, sort.order(Ascending, ""))))
		if err != nil {
			t.Fatal(err)
		}
		config := DefaultConfig()
		config.TextCollation = expr.CollationUnicode
		statement, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(query, sort.order(Ascending, config.TextCollation))))
		if err != nil || statement.SQL() != baseline.SQL() || strings.Contains(statement.SQL(), "COLLATE") {
			t.Fatal("datatable default collated UNION enum result", statement.SQL(), err)
		}
	}
}

func TestDistinctTextCollationSortsAndExports(t *testing.T) {
	spec := reportSpec()
	spec.Source = func(_ context.Context, actor reportActor) (rows.Query[reportRecord, ReportRow], error) {
		return rows.Distinct(reportSource(actor.Tenant)), nil
	}
	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{{}, {Sort: []Sort{{Column: "name", Direction: Descending}}}} {
		for _, action := range []Action{QueryAction, ExportAction} {
			var baseline string
			for _, collation := range []expr.Collation{"", expr.CollationUnicode} {
				config := DefaultConfig()
				config.TextCollation = collation
				prepared, err := table.definition.prepare(request, config)
				if err != nil {
					t.Fatal(err)
				}
				source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, action, prepared)
				if err != nil {
					t.Fatal("DISTINCT report rejected its sort default", err)
				}
				statement, err := rows.CompileRead(rows.Read(source.rows))
				if err != nil || !strings.Contains(statement.SQL(), "SELECT DISTINCT ") || strings.Contains(statement.SQL(), "COLLATE") {
					t.Fatal("DISTINCT semantics changed", statement.SQL(), err)
				}
				if collation == "" {
					baseline = statement.SQL()
				} else if statement.SQL() != baseline {
					t.Fatal("manager default changed DISTINCT ordering", statement.SQL(), baseline)
				}
			}
		}
	}
}

func TestTextCollationConfiguration(t *testing.T) {
	for _, name := range []expr.Collation{"", expr.CollationC, expr.CollationUnicode, `app"name`, expr.Collation(strings.Repeat("é", 31) + "a")} {
		config := DefaultConfig()
		config.TextCollation = name
		if err := config.Validate(); err != nil {
			t.Fatal("valid configuration rejected", err)
		}
	}
	for _, name := range []expr.Collation{"bad\x00name", "bad\nname", "bad\u0085name", "bad\xffname", expr.Collation(strings.Repeat("a", 64)), expr.Collation(strings.Repeat("é", 32)), expr.ICUCollation("en_US"), expr.ICUCollation("")} {
		config := DefaultConfig()
		config.TextCollation = name
		if manager, err := New(Dependencies{Database: new(database.DB)}, config); !errors.Is(err, fault.Invalid) || manager != nil {
			t.Fatal("invalid collation constructed a manager", err)
		}
	}
	if DefaultConfig().TextCollation != "" {
		t.Fatal("framework default must retain database ordering")
	}
}

func TestTextCollationSortsAndExports(t *testing.T) {
	for _, tc := range []struct {
		name      string
		collation expr.Collation
		sorts     []Sort
		explicit  bool
		want      string
	}{
		{"default sort", expr.CollationUnicode, nil, false, `("report_rows"."name" COLLATE "und-x-icu") ASC`},
		{"requested sort", expr.CollationUnicode, []Sort{{"name", Descending}}, false, `("report_rows"."name" COLLATE "und-x-icu") DESC`},
		{"nullable text", expr.CollationUnicode, []Sort{{"note", Ascending}}, false, `("report_rows"."note" COLLATE "und-x-icu") ASC`},
		{"decimal JSON string", expr.CollationUnicode, []Sort{{"amount", Ascending}}, false, `"report_rows"."amount" ASC`},
		{"quoted integer", expr.CollationUnicode, []Sort{{"id", Descending}}, false, `"report_rows"."id" DESC`},
		{"explicit wins", expr.CollationUnicode, nil, true, `("report_rows"."name" COLLATE "C") ASC`},
		{"database default", "", nil, false, `"report_rows"."name" ASC`},
		{"explicit without default", "", nil, true, `("report_rows"."name" COLLATE "C") ASC`},
		{"quoted name", `app"name`, nil, false, `("report_rows"."name" COLLATE "app""name") ASC`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := reportSpec()
			_, name, _, _ := reportFields()
			if tc.explicit {
				spec.Columns[1] = DefineColumn[reportRecord](validation.DefineField("name", func(row ReportRow) string { return row.Name }), "reports.name", SortBy(expr.AsSelected(expr.Collate(name, expr.CollationC))))
			}
			// Text stable ordering must remain unchanged too, not only the numeric key.
			spec.Stable = append([]expr.ProjectionOrder[reportRecord]{expr.Asc(name)}, spec.Stable...)
			table := Define(spec)
			if err := table.Validate(); err != nil {
				t.Fatal(err)
			}
			config := DefaultConfig()
			config.TextCollation = tc.collation
			prepared, err := table.definition.prepare(Request{Sort: tc.sorts}, config)
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []Action{QueryAction, ExportAction} {
				source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, action, prepared)
				if err != nil {
					t.Fatal(err)
				}
				statement, err := rows.CompileRead(rows.Read(source.rows))
				if err != nil {
					t.Fatal(err)
				}
				_, order, ok := strings.Cut(statement.SQL(), "ORDER BY ")
				want := tc.want + `, "report_rows"."name" ASC, "report_rows"."id" ASC`
				if !ok || order != want {
					t.Fatalf("%s sort: got %s, want %s", action, order, want)
				}
				count, err := rows.CompileRead(rows.Read(source.count))
				if err != nil || strings.Contains(count.SQL(), "COLLATE") {
					t.Fatal("sort default changed the count source", count.SQL(), err)
				}
			}
		})
	}
}

func TestSortCollationPreservesExpressionsAcrossManagers(t *testing.T) {
	_, name, _, _ := reportFields()
	for _, selected := range []expr.Selected[reportRecord, string]{
		expr.AsSelected(name),
		expr.AsSelected(expr.Lower(expr.Collate(name, expr.CollationC))),
	} {
		column := SortBy(selected)
		for _, collation := range []expr.Collation{expr.CollationUnicode, expr.CollationC, ""} {
			statement, err := rows.CompileRead(rows.Read(rows.OrderBy(reportSource(7), column.order(Ascending, collation))))
			if err != nil {
				t.Fatal(err)
			}
			_, order, _ := strings.Cut(statement.SQL(), "ORDER BY ")
			if strings.Contains(order, "LOWER") {
				if strings.Count(order, "COLLATE") != 1 || !strings.Contains(order, `COLLATE "C"`) {
					t.Fatal("default replaced a nested explicit collation", order)
				}
			} else if collation == "" {
				if strings.Contains(order, "COLLATE") {
					t.Fatal("one manager changed another manager's sort", order)
				}
			} else if !strings.Contains(order, `COLLATE "`+string(collation)+`"`) {
				t.Fatal("manager lost its collation", order)
			}
		}
	}
}
