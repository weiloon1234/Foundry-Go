package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type memberRow struct {
	ID   int64
	Rank int64
}

// memberQuery declares id, rank and any extra columns, which hydration discards.
func memberQuery(extra ...string) Query[memberRow] {
	columns := []Column{{Name: "id"}, {Name: "rank"}}
	for _, name := range extra {
		columns = append(columns, Column{Name: name})
	}
	return ForModel(Define("members", "id", columns, func(row database.Row) (memberRow, error) {
		var m memberRow
		destinations := []any{codec.Signed[int64]().Scan(&m.ID), codec.Signed[int64]().Scan(&m.Rank)}
		for range extra {
			destinations = append(destinations, new(any))
		}
		err := row.Scan(destinations...)
		return m, err
	}, NewModelField("id", codec.Signed[int64](), func(m memberRow) int64 { return m.ID }),
		NewModelField("rank", codec.Signed[int64](), func(m memberRow) int64 { return m.Rank })))
}

var memberColumns = []string{"label", "ok", "amount", "day", "at", "uid", "mood", "doc"}

func memberIDs(t *testing.T, ctx context.Context, executor database.Executor, q Query[memberRow]) []int64 {
	t.Helper()
	statement, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := q.OrderBy(NewOrderedField[memberRow, int64]("members", "id", codec.Signed[int64]()).Asc()).All(ctx, executor)
	if err != nil {
		t.Fatalf("%s: %v", statement.SQL(), err)
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

// One array parameter must keep each column's own equality semantics,
// including enum, uuid, numeric, date and timestamptz columns.
func TestPostgresMembershipBindsOneTypedArray(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	uid, err := model.NewID[memberRow]()
	if err != nil {
		t.Fatal(err)
	}
	amounts := make([]decimal.Decimal, 2)
	for i, text := range []string{"1.5", "3.25"} {
		if amounts[i], err = decimal.Parse(text); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`CREATE TYPE mood AS ENUM ('happy','sad')`,
		`CREATE TABLE members(id bigint PRIMARY KEY, rank bigint NOT NULL, label text NOT NULL, ok boolean NOT NULL, amount numeric NOT NULL, day date NOT NULL, at timestamptz NOT NULL, uid uuid NOT NULL, mood mood NOT NULL, doc jsonb NOT NULL)`,
		`INSERT INTO members VALUES
			(1, 10, 'a,"b}', true, 1.50, '2026-01-02', '2026-01-02 03:04:05.123456Z', '` + uid.String() + `', 'happy', '{"name":"alice"}'),
			(2, 10, 'NULL', false, 2, '2026-01-03', '2026-01-03 00:00:00Z', gen_random_uuid(), 'sad', '{"name":"bob"}'),
			(3, 20, '', true, 3.25, '2026-01-04', '2026-01-04 00:00:00Z', gen_random_uuid(), 'sad', '{"name":null}')`,
		`CREATE INDEX members_doc_name ON members ((doc ->> 'name'))`,
	} {
		if _, err := db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	var statement Statement
	day, _ := temporal.ParseDate("2026-01-03")
	at, _ := temporal.NewDateTime(time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC))
	for name, test := range map[string]struct {
		predicate Predicate[memberRow]
		want      []int64
	}{
		"integer":    {NewScalarField[memberRow, int64]("members", "rank", codec.Signed[int64]()).In(20, 99), []int64{3}},
		"text":       {NewScalarField[memberRow, string]("members", "label", codec.String[string]()).In(`a,"b}`, "NULL", ""), []int64{1, 2, 3}},
		"not in":     {NewScalarField[memberRow, string]("members", "label", codec.String[string]()).NotIn("NULL", ""), []int64{1}},
		"boolean":    {NewScalarField[memberRow, bool]("members", "ok", codec.Bool[bool]()).In(false), []int64{2}},
		"numeric":    {NewScalarField[memberRow, decimal.Decimal]("members", "amount", codec.Decimal()).In(amounts...), []int64{1, 3}},
		"date":       {NewScalarField[memberRow, temporal.Date]("members", "day", codec.Date()).In(day), []int64{2}},
		"instant":    {NewScalarField[memberRow, temporal.DateTime]("members", "at", codec.DateTime()).In(at), []int64{1}},
		"uuid":       {NewScalarField[memberRow, model.ID[memberRow]]("members", "uid", codec.ID[memberRow]()).In(uid), []int64{1}},
		"enum":       {NewScalarField[memberRow, string]("members", "mood", codec.String[string]()).NotIn("happy"), []int64{2, 3}},
		"empty not":  {NewScalarField[memberRow, int64]("members", "rank", codec.Signed[int64]()).NotIn(), []int64{1, 2, 3}},
		"empty list": {NewScalarField[memberRow, int64]("members", "rank", codec.Signed[int64]()).In(), []int64{}},
	} {
		statement, err = memberQuery(memberColumns...).Where(test.predicate).Compile()
		if err != nil {
			t.Fatal(name, err)
		}
		if len(test.want) != 0 && name != "empty not" && !strings.Contains(statement.SQL(), "ANY(") && !strings.Contains(statement.SQL(), "ALL(") {
			t.Fatalf("%s did not bind one array: %s", name, statement.SQL())
		}
		if got := memberIDs(t, ctx, db, memberQuery(memberColumns...).Where(test.predicate)); !slices.Equal(got, test.want) {
			t.Fatalf("%s matched %v, want %v", name, got, test.want)
		}
	}

	// A declared JSON property is a SQL literal, so a generic plan (the plan
	// a cached prepared statement switches to) can still use the expression index.
	doc := NewJSONField[memberRow, value.JSON[map[string]string]]("members", "doc", codec.JSON[map[string]string]())
	name := JSONTextScalar(NewJSONProperty[memberRow, map[string]string, string](JSONRoot(doc), "name", false), codec.String[string]())
	statement, err = memberQuery(memberColumns...).Where(name.Eq("alice")).Compile()
	if err != nil || len(statement.Arguments()) != 1 {
		t.Fatal(statement.SQL(), err)
	}
	err = db.Session(ctx, func(session *database.Session) error {
		defer session.Discard()
		for _, setup := range []string{"SET enable_seqscan = off", "SET plan_cache_mode = force_generic_plan", "PREPARE foundry_json_probe AS " + statement.SQL()} {
			if _, err := session.Exec(ctx, setup); err != nil {
				return err
			}
		}
		rows, err := session.Query(ctx, "EXPLAIN EXECUTE foundry_json_probe('alice')")
		if err != nil {
			return err
		}
		defer rows.Close()
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan.WriteString(line + "\n")
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !strings.Contains(plan.String(), "members_doc_name") {
			return fmt.Errorf("generic plan did not use the JSON expression index:\n%s", plan.String())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := memberIDs(t, ctx, db, memberQuery(memberColumns...).Where(name.Eq("alice"))); !slices.Equal(got, []int64{1}) {
		t.Fatal("JSON text predicate matched", got)
	}
}

// Keyset batches over a non-unique NOT NULL order column continue after the
// last (rank, id) pair: deleting an already delivered row cannot shift later
// batches, so no row is skipped or repeated at a tie boundary. Ascending order
// uses one row comparison; mixed directions use the expanded predicate.
func TestPostgresChunksContinueByKeyset(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	if _, err := db.Exec(ctx, `CREATE TABLE members(id bigint PRIMARY KEY, rank bigint NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	rank := NewOrderedField[memberRow, int64]("members", "rank", codec.Signed[int64]())
	for _, order := range []Order[memberRow]{rank.Asc(), rank.Desc(), rank.Asc().NullsFirst()} {
		if _, err := db.Exec(ctx, `DELETE FROM members`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO members SELECT i, i % 3 FROM generate_series(1, 25) AS i`); err != nil {
			t.Fatal(err)
		}
		var seen []memberRow
		err := memberQuery().OrderBy(order).Chunk(ctx, db, 4, func(batch []memberRow) error {
			if len(seen) == 0 {
				if _, err := db.Exec(ctx, `DELETE FROM members WHERE id = $1`, batch[0].ID); err != nil {
					return err
				}
			}
			seen = append(seen, batch...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := make(map[int64]bool, len(seen))
		for _, row := range seen {
			ids[row.ID] = true
		}
		if order.nulls != nullsDefault {
			// Offset batches shift after deleting a delivered row: documented.
			if len(seen) != 24 {
				t.Fatalf("offset fallback returned %d rows", len(seen))
			}
			continue
		}
		if len(seen) != 25 || len(ids) != 25 {
			t.Fatalf("keyset traversal returned %d rows, %d distinct", len(seen), len(ids))
		}
		for i := 1; i < len(seen); i++ {
			previous, current := seen[i-1], seen[i]
			ordered := previous.Rank < current.Rank
			if order.descending {
				ordered = previous.Rank > current.Rank
			}
			if !ordered && (previous.Rank != current.Rank || previous.ID >= current.ID) {
				t.Fatal("keyset traversal broke its order", previous, current)
			}
		}
	}
}

// Array length is NULL for non-arrays instead of failing, and containment at a
// path matches typed fragments only where the path exists.
func TestPostgresJSONLengthAndPathContainment(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	for _, ddl := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY, rank bigint NOT NULL, doc jsonb NOT NULL)`,
		`INSERT INTO members VALUES (1, 0, '{"tags":["a","b","c"]}'), (2, 0, '{"tags":"a"}'), (3, 0, '{}'), (4, 0, '{"tags":["b"]}')`,
	} {
		if _, err := db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	type document struct {
		Tags []string `json:"tags"`
	}
	doc := NewJSONField[memberRow, value.JSON[document]]("members", "doc", codec.JSON[document]())
	fragment, err := value.NewJSON(document{Tags: []string{"c"}})
	if err != nil {
		t.Fatal(err)
	}
	tags := NewJSONProperty[memberRow, document, []string](JSONRoot(doc), "tags", false)
	for name, test := range map[string]struct {
		predicate Predicate[memberRow]
		want      []int64
	}{
		"length":   {tags.Length().Gte(1), []int64{1, 4}},
		"longest":  {tags.Length().Gt(2), []int64{1}},
		"contains": {tags.Contains([]string{"b"}), []int64{1, 4}},
		"both":     {tags.Contains([]string{"a", "c"}), []int64{1}},
		"document": {doc.Contains(fragment), []int64{1}},
	} {
		if got := memberIDs(t, ctx, db, memberQuery("doc").Where(test.predicate)); !slices.Equal(got, test.want) {
			t.Fatalf("%s matched %v, want %v", name, got, test.want)
		}
	}
}

type memberDraft struct{ id, rank int64 }

func (d memberDraft) FoundryCreateMutation(Mutation[memberRow]) (Mutation[memberRow], error) {
	return Change(Assign[memberRow]("members", "id", codec.Signed[int64](), d.id), Assign[memberRow]("members", "rank", codec.Signed[int64](), d.rank)), nil
}

// Concurrent creators of one unique key all resolve to the committed row.
func TestPostgresInsertOrFirstResolvesConcurrentCreation(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	if _, err := db.Exec(ctx, `CREATE TABLE members(id bigint PRIMARY KEY, rank bigint NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	rank := NewScalarField[memberRow, int64]("members", "rank", codec.Signed[int64]())
	lookup := memberQuery().Where(rank.Eq(7))
	results := make(chan memberRow, 4)
	failures := make(chan error, 4)
	for i := range 4 {
		go func() {
			row, err := lookup.InsertOrFirst(ctx, db, memberDraft{id: int64(i + 1), rank: 7})
			results <- row
			failures <- err
		}()
	}
	var winner int64
	for range 4 {
		row, err := <-results, <-failures
		if err != nil {
			t.Fatal(err)
		}
		if winner == 0 {
			winner = row.ID
		}
		if row.ID != winner || row.Rank != 7 {
			t.Fatal("creators resolved to different rows", row, winner)
		}
	}
	// A conflict on a key the lookup does not select is reported, not hidden.
	if _, err := memberQuery().Where(rank.Eq(8)).InsertOrFirst(ctx, db, memberDraft{id: winner, rank: 8}); !errors.Is(err, database.UniqueViolation) {
		t.Fatal("conflict outside the lookup was hidden", err)
	}
	// Inside a caller transaction the savepoint keeps the transaction usable.
	err := db.Transaction(ctx, func(tx *database.Tx) error {
		row, err := lookup.InsertOrFirst(ctx, tx, memberDraft{id: 99, rank: 7})
		if err != nil || row.ID != winner {
			return fmt.Errorf("transaction lookup: %v %v", row, err)
		}
		_, err = tx.Exec(ctx, "SELECT 1")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
