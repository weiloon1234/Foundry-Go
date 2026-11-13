package datatable

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

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
