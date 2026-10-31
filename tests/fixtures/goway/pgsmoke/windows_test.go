package pgsmoke_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type windowRecord struct {
	ID        int64
	Amount    int64
	Missing   value.Nullable[int64]
	Day       temporal.Date
	Period    temporal.Interval
	DeletedAt value.Nullable[temporal.DateTime]
}
type windowTable struct {
	orm.Table[windowRecord, int64]
	ID        orm.Key[windowRecord, int64]
	Amount    orm.Number[windowRecord, int64]
	Missing   orm.NullableNumber[windowRecord, int64]
	Day       orm.Time[windowRecord, temporal.Date]
	Period    orm.Interval[windowRecord]
	DeletedAt orm.SoftDelete[windowRecord, temporal.DateTime]
}
type windowResult[V any] struct{ Value V }
type windowFields[V any] struct {
	rows.Row[windowResult[V]]
	Value rows.Field[windowResult[V], V]
}

func readWindow[V any](t *testing.T, ex database.Executor, d windowTable, e expr.Selected[windowRecord, V], c codec.Codec[V]) []V {
	t.Helper()
	s := rows.Define(func(b rows.Builder[windowResult[V]], r *windowResult[V]) windowFields[V] {
		return windowFields[V]{b.Row(), rows.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	out, err := rows.Read(rows.OrderBy(rows.Select(rows.FromModel(d.Query()), s, rows.MapSelected(s.Value, e)), expr.Asc(expr.Value(orm.KeyExpr(d.ID))))).All(t.Context(), ex)
	must(t, err)
	values := make([]V, len(out))
	for i, r := range out {
		values[i] = r.Value
	}
	return values
}
func TestStage6d1Windows(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE window_records(id bigint PRIMARY KEY,amount bigint NOT NULL,missing bigint,day date NOT NULL,period interval NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO window_records VALUES (1,10,NULL,'2024-01-01','1 day',NULL),(2,10,7,'2024-01-02','2 days',NULL),(3,20,8,'2024-01-04','3 days',NULL),(4,99,9,'2024-01-05','4 days',now())`)
	must(t, err)
	d := orm.Define("window_records", func(b orm.Builder[windowRecord], r *windowRecord) windowTable {
		table := orm.TableOf(b, &r.ID)
		return windowTable{table, table.Key(), orm.NumberOf(b, &r.Amount), orm.NullableNumberOf(b, &r.Missing), orm.TimeOf(b, &r.Day), orm.IntervalOf(b, &r.Period), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	w := expr.WindowFor(rows.ModelScope(d.Query())).OrderBy(expr.Asc(expr.Value(orm.KeyExpr(d.ID)))).Named("ordered")
	v := expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(d.Amount))))
	n := expr.AsSelected(expr.Row(expr.Value(orm.NullableNumberExpr(d.Missing))))
	full := w.RowsBetween(expr.UnboundedPreceding(), expr.UnboundedFollowing())
	numbers := readWindow(t, db, d, expr.RowNumber(w), codec.Signed[int64]())
	if len(numbers) != 3 || numbers[0] != 1 || numbers[2] != 3 {
		t.Fatal(numbers)
	}
	peers := expr.WindowFor(rows.ModelScope(d.Query())).OrderBy(expr.Asc(expr.Value(orm.NumberExpr(d.Amount))))
	for _, e := range []expr.Selected[windowRecord, int64]{expr.Rank(peers), expr.DenseRank(peers)} {
		r := readWindow(t, db, d, e, codec.Signed[int64]())
		if r[0] != 1 || r[1] != 1 {
			t.Fatal(r)
		}
	}
	ranks := readWindow(t, db, d, expr.Rank(peers), codec.Signed[int64]())
	dense := readWindow(t, db, d, expr.DenseRank(peers), codec.Signed[int64]())
	if ranks[2] != 3 || dense[2] != 2 {
		t.Fatal(ranks, dense)
	}
	pr := readWindow(t, db, d, expr.PercentRank(peers), codec.Float[float64]())
	cd := readWindow(t, db, d, expr.CumeDist(peers), codec.Float[float64]())
	if pr[0] != 0 || pr[2] != 1 || cd[2] != 1 {
		t.Fatal(pr, cd)
	}
	nt := readWindow(t, db, d, expr.NTile(2, w), codec.Signed[int32]())
	if nt[0] != 1 || nt[2] != 2 {
		t.Fatal(nt)
	}
	lag := readWindow(t, db, d, expr.Lag(v, 1, w), codec.Nullable(codec.Signed[int64]()))
	if !lag[0].IsNull() {
		t.Fatal(lag)
	}
	lead := readWindow(t, db, d, expr.LeadOr(v, 1, int64(99), w), codec.Signed[int64]())
	if lead[2] != 99 {
		t.Fatal(lead)
	}
	nullable := readWindow(t, db, d, expr.LagOr(n, 1, value.Of(int64(99)), w), codec.Nullable(codec.Signed[int64]()))
	if !nullable[1].IsNull() {
		t.Fatal("fallback replaced existing NULL", nullable)
	}
	for _, e := range []expr.Selected[windowRecord, value.Nullable[int64]]{expr.FirstValue(v, full), expr.NthValue(v, 2, full)} {
		got := readWindow(t, db, d, e, codec.Nullable(codec.Signed[int64]()))
		if x, ok := got[2].Get(); !ok || x != 10 {
			t.Fatal(got)
		}
	}
	last := readWindow(t, db, d, expr.LastValue(v, full), codec.Nullable(codec.Signed[int64]()))
	if x, ok := last[0].Get(); !ok || x != 20 {
		t.Fatal(last)
	}
	empty := w.RowsBetween(expr.CurrentRow(), expr.CurrentRow()).ExcludeCurrentRow()
	sums := readWindow(t, db, d, expr.Over(expr.Sum(orm.NumberExpr(d.Amount)), empty), codec.Nullable(codec.Decimal()))
	if !sums[0].IsNull() {
		t.Fatal(sums)
	}
	counts := readWindow(t, db, d, expr.Over(expr.Count[windowRecord](), empty), codec.Signed[int64]())
	if counts[0] != 0 {
		t.Fatal(counts)
	}
	filtered := expr.Filter(expr.Count[windowRecord](), expr.Gt(expr.OrderRow(expr.Value(orm.NumberExpr(d.Amount))), 10))
	counts = readWindow(t, db, d, expr.Over(filtered, full), codec.Signed[int64]())
	if counts[0] != 1 {
		t.Fatal(counts)
	}
	groups := peers.GroupsBetween(expr.CurrentRow(), expr.CurrentRow())
	counts = readWindow(t, db, d, expr.Over(expr.Count[windowRecord](), groups), codec.Signed[int64]())
	if counts[0] != 2 || counts[2] != 1 {
		t.Fatal(counts)
	}
	r := expr.NumericRange(expr.WindowFor(rows.ModelScope(d.Query())), v)
	counts = readWindow(t, db, d, expr.Over(expr.Count[windowRecord](), r.Between(r.Preceding(5), r.CurrentRow())), codec.Signed[int64]())
	if counts[0] != 2 || counts[2] != 1 {
		t.Fatal(counts)
	}
	tr := expr.TemporalRange(expr.WindowFor(rows.ModelScope(d.Query())), expr.AsSelected(expr.Row(expr.Value(orm.TimeExpr(d.Day)))))
	counts = readWindow(t, db, d, expr.Over(expr.Count[windowRecord](), tr.Between(tr.Preceding(temporal.Days(1)), tr.CurrentRow())), codec.Signed[int64]())
	if counts[1] != 2 || counts[2] != 1 {
		t.Fatal(counts)
	}
	totals := readWindow(t, db, d, expr.Over(expr.Sum(orm.NumberExpr(d.Amount)), full), codec.Nullable(codec.Decimal()))
	if x, ok := totals[0].Get(); !ok || x.String() != decimal.FromInt64(40).String() {
		t.Fatal(totals)
	}
	intervals := readWindow(t, db, d, expr.Over(expr.SumInterval(orm.IntervalExpr(d.Period)), full), codec.Nullable(codec.Interval()))
	if x, ok := intervals[0].Get(); !ok || x.String() != temporal.Days(6).String() {
		t.Fatal(intervals)
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		out := readWindow(t, tx, d, expr.RowNumber(w), codec.Signed[int64]())
		if len(out) != 3 {
			t.Fatal(out)
		}
		return nil
	}))
}
