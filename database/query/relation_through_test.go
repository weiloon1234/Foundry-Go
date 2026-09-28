package query

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func testThrough() ThroughRelation[cursorRecord, cursorRecord, cursorRecord] {
	q := cursorQuery()
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	return ManyToMany(id, id, id, id).Bind("Links", q, q, q,
		func(cursorRecord) relation.Through[cursorRecord, cursorRecord] {
			return relation.Through[cursorRecord, cursorRecord]{}
		},
		func(m cursorRecord, _ relation.Through[cursorRecord, cursorRecord]) cursorRecord { return m })
}

func TestThroughAliasedScopesAndStableOrder(t *testing.T) {
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	base := testThrough()
	r := base.Where(Or(id.Eq(1), rank.Gt(2).Not())).WherePivot(rank.Gte(3)).OrderByPivot(rank.Desc()).OrderBy(rank.Asc())
	if err := cursorQuery().With(r).Validate(); err != nil {
		t.Fatal(err)
	}
	s, err := r.compileThrough(id.In(4, 5).expression, 6)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`FROM "records" AS "foundry_target" INNER JOIN "records" AS "foundry_pivot" ON ("foundry_target"."id" = "foundry_pivot"."id")`,
		`WHERE (("foundry_target"."id" = $1) OR (NOT ("foundry_target"."rank" > $2))) AND ("foundry_pivot"."rank" >= $3) AND ("foundry_pivot"."id" IN ($4, $5))`,
		`ORDER BY "foundry_pivot"."rank" DESC, "foundry_target"."rank" ASC, "foundry_target"."id" ASC, "foundry_pivot"."id" ASC LIMIT $6`,
	} {
		if !strings.Contains(s.SQL(), want) {
			t.Fatalf("missing %s in %s", want, s.SQL())
		}
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6)}) {
		t.Fatal("joined bindings lost order")
	}
	if len(base.orders) != 0 || len(base.spec.target.predicates) != 0 || len(base.pivot.predicates) != 0 {
		t.Fatal("derivation mutated base")
	}
	captured := cursorQuery().With(&r)
	r = r.WherePivot(id.Eq(9))
	if len(captured.relations[0].(ThroughRelation[cursorRecord, cursorRecord, cursorRecord]).pivot.predicates) != 1 {
		t.Fatal("captured mutable relation pointer")
	}
}

func TestThroughInvalidDeclarations(t *testing.T) {
	base := testThrough()
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	badColumn := NewScalarField[cursorRecord, int64]("records", "missing", codec.Signed[int64]())
	badTable := NewScalarField[cursorRecord, int64]("wrong", "id", codec.Signed[int64]())
	scopedBind := base.Bind("Links", cursorQuery(), cursorQuery(), cursorQuery().Where(id.Eq(1)), base.get, base.set)
	var missing *ThroughRelation[cursorRecord, cursorRecord, cursorRecord]
	for _, r := range []Relation[cursorRecord]{missing, scopedBind, base.WherePivot(badColumn.Eq(1)), base.WherePivot(badTable.Eq(1)), base.OrderBy(badTable.Asc()), base.OrderBy(id.Asc(), id.Desc()), base.OrderByPivot(badColumn.Asc())} {
		if err := cursorQuery().With(r).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid through declaration accepted", err)
		}
	}
	// Two individually legal model scopes must still share the SELECT node cap.
	wide := make([]Predicate[cursorRecord], MaxExpressionNodes/2+1)
	for i := range wide {
		wide[i] = id.Eq(1)
	}
	if err := cursorQuery().With(base.Where(wide...).WherePivot(wide...)).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("combined expression budget ignored")
	}
	var key *ScalarField[cursorRecord, int64]
	if err := cursorQuery().With(ManyToMany(key, id, id, id)).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil many-to-many key accepted")
	}
}

type joinedTestRow struct {
	calls  int
	failAt int
	err    error
}

func (r *joinedTestRow) Scan(dest ...any) error {
	r.calls++
	if r.calls == r.failAt {
		return r.err
	}
	for i, d := range dest {
		if err := d.(sql.Scanner).Scan(int64(i + 1)); err != nil {
			return err
		}
	}
	return nil
}
func TestJoinedHydrationPublishesOnlyCompleteTuple(t *testing.T) {
	d := Define("records", "id", []Column{{Name: "id"}}, func(row database.Row) (cursorRecord, error) {
		var m cursorRecord
		err := row.Scan(codec.Signed[int64]().Scan(&m.ID))
		return m, err
	})
	row := &joinedTestRow{}
	link, err := scanJoined(row, d, d)
	if err != nil || link.Model.ID != 1 || link.Pivot.ID != 2 || row.calls != 2 {
		t.Fatal("joined fields were not partitioned", err)
	}
	want := errors.New("second decoder failed")
	link, err = scanJoined(&joinedTestRow{failAt: 2, err: want}, d, d)
	if !errors.Is(err, want) || link.Model.ID != 0 || link.Pivot.ID != 0 {
		t.Fatal("partial tuple escaped hydration failure")
	}
	var raw sql.RawBytes
	if err := (partitionedRow{row: row, count: 1, total: 1}).Scan(&raw); !errors.Is(err, fault.Invalid) {
		t.Fatal("joined decoder retained RawBytes")
	}
	if err := (partitionedRow{row: row, count: 2, total: 2}).Scan(codec.Signed[int64]().Scan(new(int64))); !errors.Is(err, fault.Invalid) {
		t.Fatal("joined decoder arity unchecked")
	}
}
