package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type transferSource struct {
	ID   int64
	Name string
}
type transferSourceTable struct {
	Table[transferSource, int64]
	Name Text[transferSource, string]
}
type transferArchive struct {
	ID   int64 `foundry:"default=database"`
	Name string
	Note value.Nullable[string]
	Tag  string
}
type transferArchiveTable struct {
	Table[transferArchive, int64]
	Name, Tag Text[transferArchive, string]
	Note      NullableText[transferArchive, string]
}

func sourceTransferTables() (transferSourceTable, transferArchiveTable) {
	s := Define("source_records", func(b Builder[transferSource], m *transferSource) transferSourceTable {
		return transferSourceTable{TableOf(b, &m.ID), TextOf(b, &m.Name)}
	})
	d := Define("archives", func(b Builder[transferArchive], m *transferArchive) transferArchiveTable {
		return transferArchiveTable{TableOf(b, &m.ID), TextOf(b, &m.Name), TextOf(b, &m.Tag), NullableTextOf(b, &m.Note)}
	})
	return s, d
}
func transferMapping[S, M, V, P any](dest ExpressionColumn[M, V, P], input ExpressionColumn[S, V, P]) ormcore.ModelMapping {
	_, d := FoundryColumn(dest, sqlowner.Metadata())
	_, s := FoundryColumn(input, sqlowner.Metadata())
	return ormcore.ModelMapping{Destination: d.Reference, Field: d.Field, Stored: reflect.TypeFor[P](), Value: ormcore.Expression{Node: s.Field, Refs: []ormcore.Reference{s.Reference}}, Err: errors.Join(d.Err, s.Err)}
}
func transferSourceMetadata(s Query[transferSource]) ormcore.Source {
	return FoundrySource(s, sqlowner.Metadata())
}
func transferInsertion(s Query[transferSource], d Query[transferArchive], maps ...InsertMapping[transferSource, transferArchive]) InsertSelect[transferSource, transferArchive] {
	return FoundryInsertFrom[transferSource](d, transferSourceMetadata(s), sqlowner.Queries()).Select(maps...)
}
func transferUpdate(s Query[transferSource], d Query[transferArchive]) UpdateSource[transferSource, transferArchive] {
	return FoundryUpdateFrom[transferSource](d, transferSourceMetadata(s), sqlowner.Queries())
}
func TestInsertSelectUsesSharedSourceCompilerAndBindings(t *testing.T) {
	s, d := sourceTransferTables()
	name := FoundryInsertMapping[transferSource, transferArchive](transferMapping(d.Name, s.Name), sqlowner.Expressions())
	nullable := name.core
	_, note := FoundryColumn(d.Note, sqlowner.Metadata())
	nullable.Field = note.Field
	nullable.Destination = note.Reference
	nullable.Stored = reflect.TypeFor[value.Nullable[string]]()
	noteMap := FoundryInsertMapping[transferSource, transferArchive](nullable, sqlowner.Expressions())
	plan := transferInsertion(s.Query().Where(s.Name.Eq("literal '; --")).OrderBy(s.Name.Asc()).Limit(3).Offset(2), d.Query(), name, noteMap).Values(d.Tag.Set(" batch "))
	statement, err := plan.plan.compile(true)
	want := `INSERT INTO "archives" ("name", "note", "tag") SELECT "source_records"."name" AS "name", "source_records"."name" AS "note", CAST($1 AS text) AS "tag" FROM "source_records" WHERE ("source_records"."name" = $2) ORDER BY "source_records"."name" ASC LIMIT $3 OFFSET $4 RETURNING "archives"."id", "archives"."name", "archives"."note", "archives"."tag"`
	if err != nil || statement.SQLText != want || !reflect.DeepEqual(statement.Bindings, []any{" batch ", "literal '; --", int64(3), int64(2)}) {
		t.Fatal(statement.SQLText, statement.Bindings, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if fmt.Sprintf(format, plan) != "orm insert from query" {
			t.Fatal("plan formatting leaked")
		}
	}
	changed, err := plan.Values(d.Tag.Set("other")).plan.compile(false)
	if err != nil || strings.Contains(changed.SQLText, "RETURNING") || changed.Bindings[0] != "other" {
		t.Fatal(changed, err)
	}
	original, err := plan.plan.compile(true)
	if err != nil || !reflect.DeepEqual(original, statement) {
		t.Fatal("derived plan changed source", err)
	}
	input := transferSourceMetadata(s.Query().Where(s.Name.Eq("in cte")))
	input.Node.Source = querycore.TableSource{Cte: &querycore.CteNode{Name: "picked", Query: input.Node, Columns: s.core.model.Columns}, Alias: "picked_records", Columns: s.core.model.Columns}
	input.Node.Selections = nil
	input.Node.Predicates = nil
	input.Owners = map[string]any{"picked_records": s.core}
	mapping := name
	mapping.core.Value.Node = querycore.FieldRef{Table: "picked_records", Column: "name"}
	mapping.core.Value.Refs = []ormcore.Reference{{Scope: reflect.TypeFor[transferSource](), Table: "picked_records", Owner: s.core}}
	fromCTE := FoundryInsertFrom[transferSource](d.Query(), input, sqlowner.Queries()).Select(mapping).Values(d.Tag.Set("outside"))
	statement, err = fromCTE.plan.compile(false)
	if err != nil || !strings.HasPrefix(statement.SQLText, `WITH "picked"`) || !reflect.DeepEqual(statement.Bindings, []any{"in cte", "outside"}) {
		t.Fatal(statement.SQLText, statement.Bindings, err)
	}
}
func TestInsertSelectRejectsIncompleteAndInvalidPlansBeforeTransaction(t *testing.T) {
	s, d := sourceTransferTables()
	foreign, _ := sourceTransferTables()
	mapping := FoundryInsertMapping[transferSource, transferArchive](transferMapping(d.Name, s.Name), sqlowner.Expressions())
	badTable := mapping
	badTable.core.Field.Table = "wrong"
	badType := mapping
	badType.core.Stored = reflect.TypeFor[int64]()
	badInput := mapping
	badInput.core.Value.Node = querycore.FieldRef{Table: "wrong", Column: "name"}
	empty := mapping
	empty.core.Value.Node = nil
	null := d.Tag.Set("x")
	null.core.AssignmentValue.Bind = func() (driver.Value, error) { return nil, nil }
	writer := &noEachWriter{}
	for label, plan := range map[string]InsertSelect[transferSource, transferArchive]{
		"missing":              transferInsertion(s.Query(), d.Query()),
		"missing metadata":     transferInsertion(s.Query(), Query[transferArchive]{}, mapping),
		"invalid source":       transferInsertion(Query[transferSource]{}, d.Query(), mapping),
		"nil source":           FoundryInsertFrom[transferSource](d.Query(), ormcore.Source{}, sqlowner.Queries()).Select(mapping),
		"filtered destination": transferInsertion(s.Query(), d.Query().Where(d.Name.Eq("filter")), mapping),
		"duplicate":            transferInsertion(s.Query(), d.Query(), mapping, mapping),
		"literal collision":    transferInsertion(s.Query(), d.Query(), mapping).Values(d.Name.Set("literal"), d.Tag.Set("tag")),
		"wrong destination":    transferInsertion(s.Query(), d.Query(), badTable),
		"wrong type":           transferInsertion(s.Query(), d.Query(), badType),
		"empty expression":     transferInsertion(s.Query(), d.Query(), empty),
		"wrong scope":          transferInsertion(s.Query(), d.Query(), badInput),
		"foreign source":       transferInsertion(foreign.Query(), d.Query(), mapping),
		"negative window":      transferInsertion(s.Query().Limit(-1), d.Query(), mapping),
		"NULL required":        transferInsertion(s.Query(), d.Query(), mapping).Values(null),
	} {
		if len(plan.plan.values) == 0 {
			plan = plan.Values(d.Tag.Set("tag"))
		}
		t.Run(label, func(t *testing.T) {
			if _, err := plan.Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) && !errors.Is(err, fault.Missing) {
				t.Fatal("invalid insertion", err)
			}
		})
	}
	plan := transferInsertion(s.Query(), d.Query(), mapping).Values(d.Tag.Set("tag"))
	for _, limit := range []int{-1, 0, querycore.MaxInsertRows + 1} {
		if _, err := plan.Returning(t.Context(), writer, limit); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid bound", err)
		}
	}
	if _, err := plan.Exec(nil, writer); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := plan.Exec(ctx, writer); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid source acquired transaction", writer.calls)
	}
}
func TestSourceWritePreservesWindowsBindingsAndCountBoundary(t *testing.T) {
	s, d := sourceTransferTables()
	key := FoundrySourceKey[transferSource, transferArchive](transferMapping(d.Key(), s.Key()), sqlowner.Expressions())
	mapping := FoundryUpdateMapping[transferSource, transferArchive](transferMapping(d.Name, s.Name), sqlowner.Expressions())
	plan := transferUpdate(s.Query().Where(s.Name.Eq("secret '; --")).OrderBy(s.Name.Asc()).Limit(2).Offset(1), d.Query().Where(d.Name.Ne("excluded"))).Match(key).Select(mapping).Values(d.Tag.Set("fixed"))
	statement, err := plan.plan.compile(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`AS MATERIALIZED (SELECT`, `COUNT(*) OVER (PARTITION BY "archives"."id")`, `LIMIT $2 OFFSET $3),`, `UPDATE "archives" SET "name" = "foundry_write_matches"."name", "tag" = $4 FROM`, `"archives"."name" <> $5`, `"archives"."id" = "foundry_write_matches"."foundry_source_key"`, `RETURNING "archives"."id", "archives"."name", "archives"."note", "archives"."tag", "foundry_write_matches"."foundry_source_matches"`} {
		if !strings.Contains(statement.SQLText, part) {
			t.Fatal(part, statement.SQLText)
		}
	}
	if !reflect.DeepEqual(statement.Bindings, []any{"secret '; --", int64(2), int64(1), "fixed", "excluded"}) || strings.Contains(statement.SQLText, "secret") {
		t.Fatal(statement.SQLText, statement.Bindings)
	}
	count, err := plan.plan.compile(false)
	if err != nil || !strings.Contains(count.SQLText, `SELECT COUNT(*), COALESCE(MAX("foundry_source_matches"), 0) FROM`) || !reflect.DeepEqual(count.Bindings, statement.Bindings) {
		t.Fatal(count, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if fmt.Sprintf(format, plan) != "orm update from query" || fmt.Sprintf(format, key) != "orm source key" || fmt.Sprintf(format, mapping) != "orm update mapping" {
			t.Fatal("descriptor leaked")
		}
	}
	if _, err := plan.Values(d.Tag.Set("new")).plan.compile(false); err != nil {
		t.Fatal(err)
	}
	unchanged, err := plan.plan.compile(true)
	if err != nil || !reflect.DeepEqual(unchanged, statement) {
		t.Fatal("derived source update changed original", err)
	}
}
func TestSourceWriteRejectsInvalidShapesBeforeTransaction(t *testing.T) {
	s, d := sourceTransferTables()
	key := FoundrySourceKey[transferSource, transferArchive](transferMapping(d.Key(), s.Key()), sqlowner.Expressions())
	mapping := FoundryUpdateMapping[transferSource, transferArchive](transferMapping(d.Name, s.Name), sqlowner.Expressions())
	base := transferUpdate(s.Query(), d.Query()).Match(key).Select(mapping)
	badKey := key
	badKey.core.Field.Column = "name"
	badType := key
	badType.core.Stored = reflect.TypeFor[string]()
	wrong := mapping
	wrong.core.Value.Node = querycore.FieldRef{Table: "elsewhere", Column: "name"}
	primary := FoundryUpdateMapping[transferSource, transferArchive](transferMapping(d.Key(), s.Key()), sqlowner.Expressions())
	writer := &noEachWriter{}
	for label, plan := range map[string]UpdateSource[transferSource, transferArchive]{
		"missing key":    transferUpdate(s.Query(), d.Query()).Select(mapping),
		"missing values": transferUpdate(s.Query(), d.Query()).Match(key),
		"not primary":    base.Match(badKey), "key type": base.Match(badType), "duplicate": base.Select(mapping),
		"overlap": base.Values(d.Name.Set("repeat")), "primary mapping": base.Select(primary), "primary literal": base.Values(d.Key().Set(1)),
		"wrong scope":        transferUpdate(s.Query(), d.Query()).Match(key).Select(wrong),
		"destination window": transferUpdate(s.Query(), d.Query().Limit(1)).Match(key).Select(mapping),
		"nil source":         FoundryUpdateFrom[transferSource](d.Query(), ormcore.Source{}, sqlowner.Queries()).Match(key).Select(mapping),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := plan.Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid update", err)
			}
		})
	}
	if _, err := base.Returning(t.Context(), writer, querycore.MaxInsertRows+1); !errors.Is(err, fault.Invalid) {
		t.Fatal("result bound", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := base.Exec(ctx, writer); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	forced := FoundryDeleteUsing[transferSource](d.Query(), transferSourceMetadata(s.Query()), true, SoftDeletable[transferArchive]{}, sqlowner.Queries()).Match(key)
	if _, err := forced.Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) {
		t.Fatal("ordinary physical special delete", err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid update acquired transaction", writer.calls)
	}
}
