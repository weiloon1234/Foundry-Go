package query

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type insertSource struct {
	id   int64
	name string
	note value.Nullable[string]
}
type insertArchive struct {
	id   int64
	name string
	note value.Nullable[string]
	tag  string
}

func insertSelectFixture() (Query[insertSource], Query[insertArchive]) {
	source := ForModel(Define("source_records", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "note", Nullable: true}}, func(database.Row) (insertSource, error) { return insertSource{}, nil }))
	destination := ForModel(Define("archives", "id", []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "note", Nullable: true}, {Name: "tag", DatabaseDefault: true}}, func(database.Row) (insertArchive, error) { return insertArchive{}, nil },
		NewModelField("id", codec.Signed[int64](), func(r insertArchive) int64 { return r.id }),
		NewModelField("name", codec.String[string](), func(r insertArchive) string { return r.name }),
		NewModelField("note", codec.Nullable(codec.String[string]()), func(r insertArchive) value.Nullable[string] { return r.note }),
		NewModelField("tag", codec.String[string](), func(r insertArchive) string { return r.tag }),
	))
	return source, destination
}

func TestInsertSelectUsesSharedSourceCompilerAndBindings(t *testing.T) {
	source, destination := insertSelectFixture()
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	name := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	note := NewNullableTextField[insertArchive, string]("archives", "note", codec.String[string]())
	plan := InsertFrom(source.Where(input.Eq("literal '; --")).OrderBy(input.Asc()).Limit(3).Offset(2), destination, MapInsert(name, input.Value()), MapInsert(note, Nullable(input.Value()))).
		Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), " batch ")))
	statement, err := plan.compile(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if text := fmt.Sprintf(format, plan); text != "model insert from query" {
			t.Fatal("insert plan exposed captured inputs", text)
		}
	}
	want := `INSERT INTO "archives" ("name", "note", "tag") SELECT "source_records"."name" AS "name", "source_records"."name" AS "note", CAST($1 AS text) AS "tag" FROM "source_records" WHERE ("source_records"."name" = $2) ORDER BY "source_records"."name" ASC LIMIT $3 OFFSET $4 RETURNING "archives"."id", "archives"."name", "archives"."note", "archives"."tag"`
	if statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{" batch ", "literal '; --", int64(3), int64(2)}) {
		t.Fatal("insert selection lost source window, exact bindings, defaults or complete result", statement, statement.Arguments())
	}
	changed := plan.Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), "other")))
	countOnly, err := changed.compile(false)
	if err != nil || strings.Contains(countOnly.SQL(), "RETURNING") || countOnly.Arguments()[0] != "other" {
		t.Fatal("count-only insert compiled incorrectly", err)
	}
	original, err := plan.compile(true)
	if err != nil || !reflect.DeepEqual(original, statement) {
		t.Fatal("derived insertion mutated original plan", err)
	}
	cte := CTE("picked", source.Where(input.Eq("in cte")))
	aliased := As[conflictAlias](cte, "picked_records")
	aliasedName := NewTextField[Alias[conflictAlias, insertSource], string]("picked_records", "name", codec.String[string]())
	fromCTE := InsertFrom(aliased, destination, MapInsert(name, aliasedName.Value())).Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), "outside")))
	statement, err = fromCTE.compile(false)
	if err != nil || !strings.HasPrefix(statement.SQL(), `WITH "picked"`) || !reflect.DeepEqual(statement.Arguments(), []any{"in cte", "outside"}) {
		t.Fatal("insert source lost CTE dependency binding order", statement, err)
	}
}

func TestInsertSelectRejectsIncompleteAndInvalidPlansBeforeTransaction(t *testing.T) {
	source, destination := insertSelectFixture()
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	name := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	mapping := MapInsert(name, input.Value())
	badTable := NewTextField[insertArchive, string]("wrong", "name", codec.String[string]())
	badType := NewOrderedField[insertArchive, int64]("archives", "name", codec.Signed[int64]())
	badInput := NewTextField[insertSource, string]("wrong", "name", codec.String[string]())
	writer := &untouchedRelationWriter{}
	for label, plan := range map[string]InsertSelect[insertSource, insertArchive]{
		"missing":                InsertFrom(source, destination),
		"missing metadata":       InsertFrom(source, For[insertArchive]("archives"), mapping),
		"nil source":             InsertFrom[insertSource](nil, destination, mapping),
		"invalid source":         InsertFrom(For[insertSource]("source_records"), destination, mapping),
		"filtered destination":   InsertFrom(source, destination.Where(name.Eq("filter")), mapping),
		"duplicate":              InsertFrom(source, destination, mapping, mapping),
		"literal collision":      InsertFrom(source, destination, mapping).Values(Change(Assign[insertArchive]("archives", "name", codec.String[string](), "literal"))),
		"wrong destination":      InsertFrom(source, destination, MapInsert(badTable, input.Value())),
		"wrong declared type":    InsertFrom(source, destination, MapInsert(badType, NewOrderedField[insertSource, int64]("source_records", "id", codec.Signed[int64]()).Value())),
		"empty expression":       InsertFrom(source, destination, MapInsert(name, Expression[insertSource, string]{})),
		"wrong SQL scope":        InsertFrom(source, destination, MapInsert(name, badInput.Value())),
		"negative source window": InsertFrom(source.Limit(-1), destination, mapping),
		"invalid literal NULL":   InsertFrom(source, destination, mapping).Values(Change(Assign[insertArchive]("archives", "tag", codec.Nullable(codec.String[string]()), value.Null[string]()))),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := plan.Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) && !errors.Is(err, fault.Missing) {
				t.Fatal("invalid insertion accepted", err)
			}
		})
	}
	plan := InsertFrom(source, destination, mapping)
	for _, limit := range []int{-1, 0, MaxInsertRows + 1} {
		if _, err := plan.Returning(t.Context(), writer, limit); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid result bound accepted", err)
		}
	}
	if _, err := plan.Exec(nil, writer); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := plan.Exec(ctx, writer); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid plan acquired a transaction", writer.calls)
	}
}

type insertTagInput struct{ text string }

func TestInsertSelectLiteralInputsNormalizeOnceWithoutBypass(t *testing.T) {
	source, destination := insertSelectFixture()
	calls := 0
	destination.definition.modelFields[3] = NewInputModelField("tag", codec.String[string](), func(r insertArchive) string { return r.tag }, func(input insertTagInput) (string, error) { calls++; return strings.TrimSpace(input.text), nil })
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	name := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	tag := NewTextField[insertArchive, string]("archives", "tag", codec.String[string]())
	plan := InsertFrom(source, destination, MapInsert(name, input.Value())).Values(Change(AssignInput[insertArchive]("archives", "tag", insertTagInput{" tag "})))
	for range 2 {
		prepared, err := plan.prepareValues(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		statement, err := prepared.compile(false)
		if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{"tag"}) {
			t.Fatal("literal insertion input bypassed normalization", err)
		}
	}
	if calls != 2 || plan.values.assignments[0].value != (insertTagInput{" tag "}) {
		t.Fatal("input transformed repeatedly or original mutated", calls)
	}
	bad := InsertFrom(source, destination, MapInsert(name, input.Value()), MapInsert(tag, input.Value()))
	if _, err := bad.Exec(t.Context(), &untouchedRelationWriter{}); !errors.Is(err, fault.Invalid) || calls != 2 {
		t.Fatal("SQL source bypassed Go field mutator", err)
	}
}

func TestInsertSelectTimestampsRespectMappedCreationAndManagedUpdate(t *testing.T) {
	calls := 0
	destination := timestampQuery(func(v temporal.DateTime) (temporal.DateTime, error) { calls++; return v.Add(time.Hour) })
	destination.definition.modelFields = append(destination.definition.modelFields, NewModelField("name", codec.String[string](), func(r timestampRecord) string { return r.name }))
	name := NewTextField[timestampRecord, string]("timed_records", "name", codec.String[string]())
	created := NewOrderedField[timestampRecord, time.Time]("timed_records", "created_on", codec.Time())
	modified := NewOrderedField[timestampRecord, temporal.DateTime]("timed_records", "modified_on", codec.DateTime())
	sourceClock := &timestampClock{now: time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	plan := InsertFrom(destination, destination, MapInsert(name, name.Value()), MapInsert(created, created.Value()))
	prepared, err := plan.prepareValues(t.Context(), sourceClock)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := prepared.compile(true)
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{sourceClock.now.UTC().Truncate(time.Microsecond).Add(time.Hour)}) || !strings.Contains(statement.SQL(), `"timed_records"."created_on" AS "created_on"`) {
		t.Fatal("insert source overwrote explicit creation or lost managed update normalization", statement, err)
	}
	if len(plan.values.assignments) != 0 || sourceClock.calls != 1 || calls != 1 {
		t.Fatal("timestamp ownership changed input plan or sampled repeatedly")
	}
	if _, err := plan.Select(MapInsert(modified, modified.Value())).prepareValues(t.Context(), sourceClock); !errors.Is(err, fault.Invalid) || sourceClock.calls != 1 {
		t.Fatal("invalid managed mapping sampled clock", err)
	}
}
