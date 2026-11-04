package rowsexprtest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestTrigramModelSQLAndLocks(t *testing.T) {
	d := declareMembers()
	search := "O'Reilly_%\\'; SELECT private"
	name := expr.Value(orm.TextExpr(d.Name))
	q := expr.Where(d.Query(), expr.TrigramMatch(name, search))
	statement, err := orm.Compile(q)
	want := `SELECT "members"."id", "members"."name", "members"."nickname" FROM "members" WHERE ("members"."name" OPERATOR(public.%) $1)`
	if err != nil || statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{search}) {
		t.Fatal(statement.SQL(), statement.Arguments(), err)
	}
	for _, tc := range []struct {
		q      orm.LockedQuery[member]
		suffix string
	}{
		{orm.Lock(q), " FOR UPDATE"},
		{orm.ForNoKeyUpdate(q).SkipLocked(), " FOR NO KEY UPDATE SKIP LOCKED"},
		{orm.ForShare(q).NoWait(), " FOR SHARE NOWAIT"},
		{orm.ForKeyShare(q).NoWait().SkipLocked().Wait(), " FOR KEY SHARE"},
	} {
		got, err := orm.CompileLocked(tc.q)
		if err != nil || got.SQL() != want+tc.suffix || !reflect.DeepEqual(got.Arguments(), []any{search}) {
			t.Fatal(got.SQL(), got.Arguments(), err)
		}
	}
}

func TestTrigramJoinedProjectionSQL(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := rows.As[left](m.Query(), "m"), rows.As[right](o.Query(), "o")
	joined := rows.InnerJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.NumberExpr(o.MemberID))))
	name := expr.At(rows.LeftScope(joined, rows.Scope(a)), orm.TextExpr(m.Name))
	score := expr.TrigramSimilarity(name, "word")
	q := rows.WhereScalar(rows.SelectScalar(joined, expr.AsSelected(score)), expr.TrigramMatch(name, "word"), expr.Gte(score, 0.6))
	statement, err := rows.CompileRead(rows.ReadScalar(q))
	want := `SELECT public.similarity("m"."name", $1) AS "value" FROM "members" AS "m" INNER JOIN "orders" AS "o" ON ("m"."id" = "o"."member_id") WHERE ("m"."name" OPERATOR(public.%) $2) AND (public.similarity("m"."name", $3) >= $4)`
	if err != nil || statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{"word", "word", "word", 0.6}) {
		t.Fatal(statement.SQL(), statement.Arguments(), err)
	}
	ordered, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(q, expr.Desc(score))))
	if err != nil || !strings.Contains(ordered.SQL(), "ORDER BY public.similarity(") || !strings.HasSuffix(ordered.SQL(), " DESC") {
		t.Fatal(ordered.SQL(), err)
	}
}

func TestTrigramNullableAndSelectedSQL(t *testing.T) {
	d := declareAggregateRecords()
	input := expr.Value(orm.NullableTextExpr(d.MaybeName))
	score := expr.TrigramSimilarityNullable(input, aggregateName("word"))
	q := rows.WhereScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(score)), expr.NullableGte(score, 0.6))
	statement, err := rows.CompileRead(rows.ReadScalar(q))
	want := `SELECT public.similarity("aggregate_records"."maybe_name", $1) AS "value" FROM "aggregate_records" WHERE (public.similarity("aggregate_records"."maybe_name", $2) >= $3)`
	if err != nil || statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{"word", "word", 0.6}) {
		t.Fatal(statement.SQL(), statement.Arguments(), err)
	}
	selected := expr.TrigramSimilarityNullableValue(expr.AsSelected(expr.Min(orm.TextExpr(d.Name))), aggregateName("word"))
	statement, err = rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(d.Query()), selected)))
	if err != nil || statement.SQL() != `SELECT public.similarity(MIN("aggregate_records"."name"), $1) AS "value" FROM "aggregate_records"` {
		t.Fatal(statement.SQL(), err)
	}
}

func TestTrigramInvalidInputBeforeIO(t *testing.T) {
	d, foreign := declareAggregateRecords(), declareAggregateRecords()
	source := rows.From(d.Query())
	for _, input := range []expr.RowValue[aggregateRecord, aggregateName]{nil, expr.RowExpression[aggregateRecord, aggregateName]{}, expr.Value(orm.TextExpr(foreign.Name)), expr.Constant[aggregateRecord](codec.String[aggregateName]().WithParameterType(codec.TypeInteger), "word")} {
		rejectScalar(t, source, expr.AsSelected(expr.TrigramSimilarity(input, aggregateName("word"))), codec.Float[float64]())
		state := &driverState{}
		_, err := orm.All(t.Context(), openTest(t, state), expr.Where(d.Query(), expr.TrigramMatch(input, aggregateName("word"))))
		if !errors.Is(err, fault.Invalid) || state.sql != "" {
			t.Fatal("invalid match reached I/O", err, state.sql)
		}
	}
	rejectScalar(t, source, expr.AsSelected(expr.TrigramSimilarityNullable[aggregateRecord, aggregateName](nil, "word")), codec.Nullable(codec.Float[float64]()))
}
