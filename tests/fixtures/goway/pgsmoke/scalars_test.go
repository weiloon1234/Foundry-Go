package pgsmoke_test

import (
	"testing"
	"time"

	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func readScalar[V any](t *testing.T, ex database.Executor, v expr.Selected[scalars.Record, V], c codec.Codec[V]) V {
	t.Helper()
	s := rows.Define(func(b rows.Builder[windowResult[V]], r *windowResult[V]) windowFields[V] {
		return windowFields[V]{b.Row(), rows.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	out, err := rows.Read(rows.Select(rows.FromModel(scalars.Records.Query().Where(orm.NumberEq(scalars.Records.Millis, 0))), s, rows.MapSelected(s.Value, v))).All(t.Context(), ex)
	must(t, err)
	if len(out) != 1 {
		t.Fatalf("scope lost: %d rows", len(out))
	}
	return out[0].Value
}
func TestStage6d2Scalars(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE scalar_records(id bigint PRIMARY KEY,name text NOT NULL,maybe_name text,date date NOT NULL,maybe_date date,local timestamp NOT NULL,maybe_local timestamp,instant timestamptz NOT NULL,maybe_instant timestamptz,clock time NOT NULL,maybe_clock time,period interval NOT NULL,maybe_period interval,millis bigint NOT NULL,maybe_millis bigint,latitude double precision NOT NULL,maybe_latitude double precision)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO scalar_records(id,name,date,local,instant,clock,period,millis,latitude) VALUES (1,' HéLLo_世界% ','2024-02-29','2024-02-29 12:34:56.123456','2024-03-10 06:30:00+00','23:59:59.123456','1 mon 2 days 3 seconds',0,0),(2,'ignored','2024-01-01','2024-01-01','2024-01-01 00:00:00+00','00:00','0 days',1,1)`)
	must(t, err)
	d := scalars.Records
	name := expr.Lower(expr.Trim(expr.Value(orm.TextExpr(d.Name))))
	if got := readScalar(t, db, expr.AsSelected(name), codec.String[string]()); got != "héllo_世界%" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.Length(name)), codec.Signed[int64]()); got != 9 {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.Substring(name, 7, 2)), codec.String[string]()); got != "世界" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.LowerNullable(expr.Value(orm.NullableTextExpr(d.MaybeName)))), codec.Nullable(codec.String[string]())); !got.IsNull() {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.ConcatWSNullable("/", expr.Value(orm.NullableTextExpr(d.MaybeName)))), codec.String[string]()); got != "" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.Year(expr.Value(orm.TimeExpr(d.Date)))), codec.Signed[int64]()); got != 2024 {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.Second(expr.Value(orm.TimeExpr(d.Clock)))), codec.Decimal()); got.String() != "59.123456" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.AddDateInterval(expr.Value(orm.TimeExpr(d.Date)), temporal.Days(1))), codec.LocalDateTime()); got.String() != "2024-03-01T00:00:00" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.AddClockElapsed(expr.Value(orm.TimeExpr(d.Clock)), time.Second)), codec.WallTime()); got.String() != "00:00:00.123456" {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.IntervalMonths(expr.Value(orm.IntervalExpr(d.Period)))), codec.Signed[int32]()); got != 1 {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.IntervalDays(expr.Value(orm.IntervalExpr(d.Period)))), codec.Signed[int32]()); got != 2 {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.IntervalElapsed(expr.Value(orm.IntervalExpr(d.Period)))), codec.Signed[time.Duration]()); got != 3*time.Second {
		t.Fatal(got)
	}
	zone, err := expr.LoadTimeZone("America/New_York")
	must(t, err)
	shifted := expr.AddInstantInterval(expr.Value(orm.TimeExpr(d.Instant)), temporal.Days(1), zone)
	if got := readScalar(t, db, expr.AsSelected(expr.IntervalElapsed(expr.InstantDifference(shifted, expr.Value(orm.TimeExpr(d.Instant))))), codec.Signed[time.Duration]()); got != 23*time.Hour {
		t.Fatal(got)
	}
	if got := readScalar(t, db, expr.AsSelected(expr.SphericalDistance(expr.Value(orm.NumberExpr(d.Latitude)), expr.Value(orm.NumberExpr(d.Latitude)), expr.GeographicPoint{})), codec.Float[float64]()); got != 0 {
		t.Fatal(got)
	}
	must(t, db.Transaction(ctx, func(tx *database.Tx) error {
		clock := expr.AsSelected(expr.TransactionTime(rows.ModelScope(d.Query())))
		a := readScalar(t, tx, clock, codec.DateTime())
		b := readScalar(t, tx, clock, codec.DateTime())
		if a != b {
			t.Fatal("transaction clock changed", a, b)
		}
		return nil
	}))
}
