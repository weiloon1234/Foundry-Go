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
)

func sourceWriteFixture() (Query[insertSource], Query[insertArchive], SourceKey[insertSource, insertArchive], UpdateMapping[insertSource, insertArchive]) {
	source, destination := insertSelectFixture()
	key := NewOrderedField[insertArchive, int64]("archives", "id", codec.Signed[int64]())
	id := NewOrderedField[insertSource, int64]("source_records", "id", codec.Signed[int64]())
	name := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	return source, destination, MatchSource(key, id.Value()), MapUpdate(name, input.Value())
}

func TestSourceWritePreservesWindowsBindingsAndCountBoundary(t *testing.T) {
	source, destination, key, mapping := sourceWriteFixture()
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	name := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	source = source.Where(input.Eq("secret '; --")).OrderBy(input.Asc()).Limit(2).Offset(1)
	plan := UpdateFrom(destination.Where(name.Ne("excluded")), source).Match(key).Select(mapping).
		Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), "fixed")))
	statement, err := plan.plan.compile(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`AS MATERIALIZED (SELECT`, `COUNT(*) OVER (PARTITION BY "archives"."id")`, `LIMIT $2 OFFSET $3),`, `UPDATE "archives" SET "name" = "foundry_write_matches"."name", "tag" = $4 FROM`, `"archives"."name" <> $5`, `"archives"."id" = "foundry_write_matches"."foundry_source_key"`, `RETURNING "archives"."id", "archives"."name", "archives"."note", "archives"."tag", "foundry_write_matches"."foundry_source_matches"`} {
		if !strings.Contains(statement.SQL(), part) {
			t.Fatalf("missing %s in %s", part, statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{"secret '; --", int64(2), int64(1), "fixed", "excluded"}) {
		t.Fatal("source/destination binding order changed", statement.Arguments())
	}
	if strings.Contains(statement.SQL(), "secret") {
		t.Fatal("source parameter interpolated into SQL")
	}
	count, err := plan.plan.compile(false)
	if err != nil || !strings.Contains(count.SQL(), `SELECT COUNT(*), COALESCE(MAX("foundry_source_matches"), 0) FROM`) || !reflect.DeepEqual(count.Arguments(), statement.Arguments()) {
		t.Fatal("count-only source validation lost aggregate boundary", count, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if fmt.Sprintf(format, plan) != "model update from query" || fmt.Sprintf(format, key) != "model source key" || fmt.Sprintf(format, mapping) != "model update mapping" {
			t.Fatal("source descriptors exposed captured values")
		}
	}
	derived := plan.Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), "new")))
	if _, err := derived.plan.compile(false); err != nil {
		t.Fatal(err)
	}
	unchanged, err := plan.plan.compile(true)
	if err != nil || !reflect.DeepEqual(statement, unchanged) {
		t.Fatal("derived source update changed original")
	}
}

func TestSourceWriteRejectsInvalidShapesBeforeTransaction(t *testing.T) {
	source, destination, key, mapping := sourceWriteFixture()
	base := UpdateFrom(destination, source).Match(key).Select(mapping)
	id := NewOrderedField[insertArchive, int64]("archives", "id", codec.Signed[int64]())
	input := NewOrderedField[insertSource, int64]("source_records", "id", codec.Signed[int64]())
	badKey := key
	badKey.field.column = "name"
	badType := key
	badType.typ = reflect.TypeFor[string]()
	wrongScope := mapping
	wrongScope.expression = fieldRef{"elsewhere", "name"}
	writer := &untouchedRelationWriter{}
	for label, plan := range map[string]UpdateSource[insertSource, insertArchive]{
		"missing key":        UpdateFrom(destination, source).Select(mapping),
		"missing values":     UpdateFrom(destination, source).Match(key),
		"key is not primary": base.Match(badKey),
		"key type":           base.Match(badType),
		"duplicate":          base.Select(mapping),
		"overlap":            base.Values(Change(Assign[insertArchive]("archives", "name", codec.String[string](), "repeat"))),
		"primary mapping":    base.Select(MapUpdate(id, input.Value())),
		"primary literal":    base.Values(Change(Assign[insertArchive]("archives", "id", codec.Signed[int64](), int64(1)))),
		"wrong scope":        UpdateFrom(destination, source).Match(key).Select(wrongScope),
		"destination window": UpdateFrom(destination.Limit(1), source).Match(key).Select(mapping),
		"nil source":         UpdateFrom[insertSource](destination, nil).Match(key).Select(mapping),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := plan.Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid source write accepted", err)
			}
		})
	}
	if writer.calls != 0 {
		t.Fatal("invalid source write opened a transaction")
	}
	if _, err := base.Returning(t.Context(), writer, MaxInsertRows+1); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded model return accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := base.Exec(ctx, writer); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := ForceDeleteUsing(destination, source).Match(key).Exec(t.Context(), writer); !errors.Is(err, fault.Invalid) {
		t.Fatal("physical special deletion accepted non-soft model", err)
	}
}

func TestSourceWritesShareLiteralMutatorsAndSoftDeleteConventions(t *testing.T) {
	source, destination, key, _ := sourceWriteFixture()
	calls := 0
	destination.definition.modelFields[3] = NewInputModelField("tag", codec.String[string](), func(r insertArchive) string { return r.tag }, func(v insertTagInput) (string, error) { calls++; return strings.TrimSpace(v.text), nil })
	plan := UpdateFrom(destination, source).Match(key).Values(Change(AssignInput[insertArchive]("archives", "tag", insertTagInput{" tag "})))
	for range 2 {
		prepared, err := plan.plan.prepareValues(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		statement, err := prepared.compile(false)
		if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{"tag"}) || strings.Contains(statement.SQL(), "COUNT(*) OVER") {
			t.Fatal("fixed update lost normalization or unnecessarily rejects duplicate source keys", statement, err)
		}
	}
	if calls != 2 || plan.plan.values.assignments[0].value != (insertTagInput{" tag "}) {
		t.Fatal("source update changed original or repeatedly normalized it")
	}
	tag := NewTextField[insertArchive, string]("archives", "tag", codec.String[string]())
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	if _, err := UpdateFrom(destination, source).Match(key).Select(MapUpdate(tag, input.Value())).Exec(t.Context(), &untouchedRelationWriter{}); !errors.Is(err, fault.Invalid) || calls != 2 {
		t.Fatal("source SQL bypassed Go mutator", err)
	}
	soft := softQuery()
	id := softID()
	clock := &timestampClock{now: time.Date(2033, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	deletion := DeleteUsing(soft.WithTrashed(), soft.WithTrashed()).Match(MatchSource(id, id.Value()))
	prepared, err := deletion.plan.prepareValues(t.Context(), clock)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := prepared.compile(false)
	if err != nil || !strings.Contains(statement.SQL(), `UPDATE "soft_records" SET "deleted_on" = $1 FROM`) || !strings.Contains(statement.SQL(), `"soft_records"."deleted_on" IS NULL`) || !reflect.DeepEqual(statement.Arguments(), []any{clock.now.UTC().Truncate(time.Microsecond)}) || clock.calls != 1 {
		t.Fatal("source removal lost active-only scope or owning clock", statement, err)
	}
	force := ForceDeleteUsing(soft.WithTrashed(), soft.WithTrashed()).Match(MatchSource(id, id.Value()))
	prepared, err = force.plan.prepareValues(t.Context(), clock)
	if err != nil {
		t.Fatal(err)
	}
	statement, err = prepared.compile(true)
	if err != nil || !strings.Contains(statement.SQL(), `DELETE FROM "soft_records" USING`) || strings.Contains(statement.SQL(), "IS NULL") || clock.calls != 1 {
		t.Fatal("force source deletion sampled clock or changed explicit scope", statement, err)
	}
}

type sourceCountRow struct {
	count int64
	err   error
}

func (r sourceCountRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	*values[len(values)-1].(*int64) = r.count
	return nil
}
func TestSourceMatchDecoderRejectsAmbiguityAndPreservesScanErrors(t *testing.T) {
	errScan := errors.New("scan failed")
	for _, test := range []struct {
		row  sourceCountRow
		want error
	}{{sourceCountRow{count: 1}, nil}, {sourceCountRow{count: 2}, database.TooManyRows}, {sourceCountRow{count: 0}, fault.Invalid}, {sourceCountRow{err: errScan}, errScan}} {
		if err := (sourceMatchRow{Row: test.row}).Scan(new(string)); !errors.Is(err, test.want) {
			t.Fatal("source match decoding lost validation", err, test.want)
		}
	}
}

func TestSourceWriteCTEsAvoidPrivateNameCollisions(t *testing.T) {
	source, destination, _, _ := sourceWriteFixture()
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	aliased := As[conflictAlias](CTE("foundry_write_source", source.Where(input.Eq("cte-input"))), "foundry_selected_rows")
	id := NewOrderedField[Alias[conflictAlias, insertSource], int64]("foundry_selected_rows", "id", codec.Signed[int64]())
	name := NewTextField[Alias[conflictAlias, insertSource], string]("foundry_selected_rows", "name", codec.String[string]())
	key := NewOrderedField[insertArchive, int64]("archives", "id", codec.Signed[int64]())
	targetName := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	statement, err := UpdateFrom(destination, aliased).Match(MatchSource(key, id.Value())).Select(MapUpdate(targetName, name.Value())).plan.compile(false)
	if err != nil || !strings.HasPrefix(statement.SQL(), `WITH "foundry_write_source"`) || !strings.Contains(statement.SQL(), `"foundry_write_source_2"`) || !reflect.DeepEqual(statement.Arguments(), []any{"cte-input"}) {
		t.Fatal("source write lost CTE dependency or private alias isolation", statement, err)
	}
}

func TestSourceWriteUsesManagedUpdateTimeWithoutChangingCreation(t *testing.T) {
	q := timestampQuery(nil)
	q.definition.modelFields[1] = NewModelField("modified_on", codec.DateTime(), func(r timestampRecord) temporal.DateTime { return r.updated })
	q.definition.modelFields = append(q.definition.modelFields, NewModelField("id", codec.Signed[int](), func(r timestampRecord) int { return r.id }), NewModelField("name", codec.String[string](), func(r timestampRecord) string { return r.name }))
	id := NewOrderedField[timestampRecord, int]("timed_records", "id", codec.Signed[int]())
	created := NewOrderedField[timestampRecord, time.Time]("timed_records", "created_on", codec.Time())
	updated := NewOrderedField[timestampRecord, temporal.DateTime]("timed_records", "modified_on", codec.DateTime())
	clock := &timestampClock{now: time.Date(2034, 1, 2, 3, 4, 5, 123456789, time.UTC)}
	plan := UpdateFrom(q, q).Match(MatchSource(id, id.Value())).Select(MapUpdate(created, created.Value()))
	prepared, err := plan.plan.prepareValues(t.Context(), clock)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := prepared.compile(false)
	if err != nil || clock.calls != 1 || !reflect.DeepEqual(statement.Arguments(), []any{clock.now.UTC().Truncate(time.Microsecond)}) || !strings.Contains(statement.SQL(), `"created_on" = "foundry_write_matches"."created_on"`) || len(plan.plan.values.assignments) != 0 {
		t.Fatal("source update lost timestamp ownership or changed original", statement, err)
	}
	if _, err := plan.Select(MapUpdate(updated, updated.Value())).plan.prepareValues(t.Context(), clock); !errors.Is(err, fault.Invalid) || clock.calls != 1 {
		t.Fatal("mapped update timestamp bypassed owning clock", err)
	}
}
