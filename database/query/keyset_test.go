package query

import (
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestCountOmitsOrderingThatCannotChangeIt(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	ordered := cursorQuery().OrderBy(id.Desc())
	statement, err := ordered.compile(readCount)
	if err != nil || strings.Contains(statement.SQL(), "ORDER BY") {
		t.Fatal("count retained an irrelevant ORDER BY", statement.SQL(), err)
	}
	for _, windowed := range []Query[cursorRecord]{ordered.Limit(5), ordered.Offset(2)} {
		statement, err := windowed.compile(readCount)
		if err != nil || !strings.Contains(statement.SQL(), "ORDER BY") {
			t.Fatal("count of a Limit/Offset window lost its ORDER BY", statement.SQL(), err)
		}
	}
}

func TestKeysetPredicateUsesRowComparisonOnlyForNotNullKeys(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	notNull := func(Order[cursorRecord]) bool { return false }
	compile := func(p Predicate[cursorRecord]) string {
		statement, err := cursorQuery().Where(p).Compile()
		if err != nil {
			t.Fatal(err)
		}
		return statement.SQL()
	}
	keys := []driver.Value{int64(7), int64(42)}
	sql := compile(cursorPredicate([]Order[cursorRecord]{rank.Asc(), id.Asc()}, keys, notNull))
	if !strings.Contains(sql, `(("records"."rank", "records"."id") > ($1, $2))`) {
		t.Fatal("NOT NULL keys in one direction did not use a row comparison", sql)
	}
	sql = compile(cursorPredicate([]Order[cursorRecord]{rank.Desc(), id.Desc()}, keys, notNull))
	if !strings.Contains(sql, `(("records"."rank", "records"."id") < ($1, $2))`) {
		t.Fatal("descending row comparison", sql)
	}
	sql = compile(cursorPredicate([]Order[cursorRecord]{rank.Asc(), id.Desc()}, keys, notNull))
	if strings.Contains(sql, ") > (") || strings.Contains(sql, "IS NULL") {
		t.Fatal("mixed directions must expand without NULL alternatives for NOT NULL keys", sql)
	}
	sql = compile(cursorPredicate([]Order[cursorRecord]{rank.Asc(), id.Asc()}, keys, cursorQuery().definition.nullableOrder))
	if strings.Contains(sql, ") > (") || !strings.Contains(sql, `"records"."rank" IS NULL`) {
		t.Fatal("nullable key lost its NULL alternative", sql)
	}
	p, err := cursorQuery().OrderBy(rank.Asc()).chunkPlan(2, false)
	if err != nil || len(p.keys) != 2 {
		t.Fatal("nullable order was not keyed", err)
	}
	if more, err := p.advance([]cursorRecord{{ID: 1}, {ID: 2}}); err != nil || !more || !reflect.DeepEqual(p.after, []driver.Value{nil, int64(2)}) || p.offset != 0 {
		t.Fatal("keyset did not continue from the last NULL key", p.after, err)
	}
}

// A window relative to the database's transaction time binds the same values
// on every request, so cursor fingerprints stay stable across pages.
func TestRelativeTimeWindowBindsStableValues(t *testing.T) {
	at := NewOrderedField[cursorRecord, temporal.DateTime]("records", "rank", codec.DateTime())
	elapsed, err := temporal.Elapsed(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	q := cursorQuery()
	compile := func() Statement {
		since := SubtractInstantInterval(TransactionTime(q), elapsed, UTCZone())
		statement, err := q.Where(Greater(at, since)).Compile()
		if err != nil {
			t.Fatal(err)
		}
		return statement
	}
	first, second := compile(), compile()
	if first.SQL() != second.SQL() || !reflect.DeepEqual(first.Arguments(), second.Arguments()) {
		t.Fatal("relative window bindings changed between requests")
	}
}
