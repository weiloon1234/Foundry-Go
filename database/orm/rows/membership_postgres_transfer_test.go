package rows

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type membershipDocument struct {
	Name value.Nullable[string] `json:"name"`
}
type membershipRow struct {
	ID     int64
	Rank   int64
	Label  string
	OK     bool `column:"ok"`
	Amount decimal.Decimal
	Day    temporal.Date
	At     temporal.DateTime
	UID    model.ID[membershipRow] `column:"uid"`
	Mood   string
	Doc    value.JSON[membershipDocument]
}
type membershipTable struct {
	orm.Table[membershipRow, int64]
	Rank   orm.Number[membershipRow, int64]
	Label  orm.Text[membershipRow, string]
	OK     orm.Column[membershipRow, bool]
	Amount orm.Number[membershipRow, decimal.Decimal]
	Day    orm.Time[membershipRow, temporal.Date]
	At     orm.Time[membershipRow, temporal.DateTime]
	UID    orm.Column[membershipRow, model.ID[membershipRow]]
	Mood   orm.Text[membershipRow, string]
	Doc    orm.JSON[membershipRow, membershipDocument]
}

func membershipFields() membershipTable {
	return orm.Define("members", func(b orm.Builder[membershipRow], m *membershipRow) membershipTable {
		return membershipTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank), orm.TextOf(b, &m.Label), orm.BoolOf(b, &m.OK), orm.NumberOf(b, &m.Amount), orm.TimeOf(b, &m.Day), orm.TimeOf(b, &m.At), orm.ColumnOf(b, &m.UID, orm.Codec(codec.ID[membershipRow]())), orm.TextOf(b, &m.Mood), orm.JSONOf(b, &m.Doc)}
	})
}
func membershipIDs(t *testing.T, ctx context.Context, ex database.Executor, q orm.Query[membershipRow], d membershipTable) []int64 {
	t.Helper()
	selected := SelectScalar(From(q), expr.Row(expr.Value(d.Key())).Value())
	got, e := ReadScalar(OrderScalar(selected, expr.Value(d.Key()).Asc())).All(ctx, ex)
	if e != nil {
		t.Fatal(e)
	}
	return got
}
func TestPostgresMembershipBindsOneTypedArray(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	ctx := t.Context()
	uid, err := model.NewIDAt[membershipRow](time.Now())
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
	d := membershipFields()
	var statement orm.Statement
	day, _ := temporal.ParseDate("2026-01-03")
	at, _ := temporal.NewDateTime(time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC))
	for name, test := range map[string]struct {
		predicate orm.Predicate[membershipRow]
		want      []int64
	}{
		"integer":    {d.Rank.In(20, 99), []int64{3}},
		"text":       {d.Label.In(`a,"b}`, "NULL", ""), []int64{1, 2, 3}},
		"not in":     {d.Label.NotIn("NULL", ""), []int64{1}},
		"boolean":    {d.OK.In(false), []int64{2}},
		"numeric":    {d.Amount.In(amounts...), []int64{1, 3}},
		"date":       {d.Day.In(day), []int64{2}},
		"instant":    {d.At.In(at), []int64{1}},
		"uuid":       {d.UID.In(uid), []int64{1}},
		"enum":       {d.Mood.NotIn("happy"), []int64{2, 3}},
		"empty not":  {d.Rank.NotIn(), []int64{1, 2, 3}},
		"empty list": {d.Rank.In(), []int64{}},
	} {
		statement, err = orm.Compile(d.Query().Where(test.predicate))
		if err != nil {
			t.Fatal(name, err)
		}
		if len(test.want) != 0 && name != "empty not" && (len(statement.Arguments()) != 1 || (!strings.Contains(statement.SQL(), "ANY(") && !strings.Contains(statement.SQL(), "ALL("))) {
			t.Fatalf("%s did not bind one array: %s", name, statement.SQL())
		}
		if got := membershipIDs(t, ctx, db, d.Query().Where(test.predicate), d); !slices.Equal(got, test.want) {
			t.Fatalf("%s matched %v, want %v", name, got, test.want)
		}
	}

	// A declared JSON property is a SQL literal, so a generic plan (the plan
	// a cached prepared statement switches to) can still use the expression index.
	name := expr.JSONTextScalar(expr.JSONNullable(expr.JSONProperty(expr.JSONRoot(expr.Value(d.Doc)), expr.JSONFieldOf(func(p *membershipDocument) *value.Nullable[string] { return &p.Name }))), codec.String[string]())
	statement, err = CompileRead(ReadScalar(SelectScalar(From(expr.Where(d.Query(), name.Eq("alice"))), expr.Row(expr.Value(d.Key())).Value())))
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
	if got := membershipIDs(t, ctx, db, expr.Where(d.Query(), name.Eq("alice")), d); !slices.Equal(got, []int64{1}) {
		t.Fatal("JSON text predicate matched", got)
	}
}

func TestPostgresChunksContinueByKeyset(t *testing.T) {
	db := pgtest.Isolate(t).Open(t)
	ctx := t.Context()
	type record struct{ ID, Rank int64 }
	type fields struct {
		orm.Table[record, int64]
		Rank orm.Number[record, int64]
	}
	d := orm.Define("members", func(b orm.Builder[record], m *record) fields {
		return fields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank)}
	})
	if _, e := db.Exec(ctx, `CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	for index, order := range []orm.Order[record]{d.Rank.Asc(), d.Rank.Desc(), d.Rank.Asc().NullsFirst()} {
		if _, e := db.Exec(ctx, `DELETE FROM members`); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec(ctx, `INSERT INTO members SELECT i,i % 3 FROM generate_series(1,25) AS i`); e != nil {
			t.Fatal(e)
		}
		var seen []record
		err := d.Query().OrderBy(order).Chunk(ctx, db, 4, func(batch []record) error {
			if len(seen) == 0 {
				if _, e := db.Exec(ctx, `DELETE FROM members WHERE id=$1`, batch[0].ID); e != nil {
					return e
				}
			}
			seen = append(seen, batch...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[int64]bool{}
		for _, m := range seen {
			ids[m.ID] = true
		}
		if index == 2 {
			if len(seen) != 24 {
				t.Fatal("offset fallback did not shift after deletion", len(seen))
			}
			continue
		}
		if len(seen) != 25 || len(ids) != 25 {
			t.Fatal("keyset lost or repeated row", len(seen), len(ids))
		}
		for i := 1; i < len(seen); i++ {
			p, c := seen[i-1], seen[i]
			ordered := p.Rank < c.Rank
			if index == 1 {
				ordered = p.Rank > c.Rank
			}
			if !ordered && (p.Rank != c.Rank || p.ID >= c.ID) {
				t.Fatal("keyset order", p, c)
			}
		}
	}
}

func TestPostgresJSONLengthAndPathContainment(t *testing.T) {
	db := pgtest.Isolate(t).Open(t)
	ctx := t.Context()
	for _, sql := range []string{`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL,doc jsonb NOT NULL)`, `INSERT INTO members VALUES (1,0,'{"tags":["a","b","c"]}'),(2,0,'{"tags":"a"}'),(3,0,'{}'),(4,0,'{"tags":["b"]}')`} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	type document struct {
		Tags []string `json:"tags"`
	}
	type record struct {
		ID, Rank int64
		Doc      value.JSON[document]
	}
	type fields struct {
		orm.Table[record, int64]
		Rank orm.Number[record, int64]
		Doc  orm.JSON[record, document]
	}
	d := orm.Define("members", func(b orm.Builder[record], m *record) fields {
		return fields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank), orm.JSONOf(b, &m.Doc)}
	})
	tags := expr.JSONProperty(expr.JSONRoot(expr.Value(d.Doc)), expr.JSONFieldOf(func(p *document) *[]string { return &p.Tags }))
	fragment, err := value.NewJSON(document{Tags: []string{"c"}})
	if err != nil {
		t.Fatal(err)
	}
	contains := func(items ...string) expr.Predicate[record] {
		v, e := value.NewJSON(items)
		if e != nil {
			t.Fatal(e)
		}
		return expr.JSONContainsNullable(tags.JSON(), tags.JSON().Param(value.Of(v))).Eq(value.Of(true))
	}
	for name, test := range map[string]struct {
		p    expr.Predicate[record]
		want []int64
	}{
		"length": {expr.JSONLengthNullable(tags.JSON()).Gte(1), []int64{1, 4}}, "longest": {expr.JSONLengthNullable(tags.JSON()).Gt(2), []int64{1}}, "contains": {contains("b"), []int64{1, 4}}, "both": {contains("a", "c"), []int64{1}}, "document": {expr.JSONContains(expr.Value(d.Doc), expr.Param(d.Doc, fragment)).Eq(true), []int64{1}},
	} {
		q := OrderScalar(WhereScalar(SelectScalar(From(d.Query()), expr.Row(expr.Value(d.Key())).Value()), test.p), expr.Value(d.Key()).Asc())
		got, e := ReadScalar(q).All(ctx, db)
		if e != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatal(name, got, e)
		}
	}
}
