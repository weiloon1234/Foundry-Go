package pgsmoke_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type codecRow[V any] struct {
	ID    int64
	Value V
	Maybe value.Nullable[V]
}
type codecFields[V any] struct {
	orm.Table[codecRow[V], int64]
	Value orm.Column[codecRow[V], V]
	Maybe orm.NullableColumn[codecRow[V], V]
}

// Every call owns a distinct table inside the Isolate schema; nothing is reset.
func roundTripKind[V any](t *testing.T, db *database.DB, name, sqlType string, input V, c codec.Codec[V]) {
	t.Helper()
	fields := orm.Define("codec_"+name, func(b orm.Builder[codecRow[V]], m *codecRow[V]) codecFields[V] {
		return codecFields[V]{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.Value, orm.Codec(c)), orm.NullableColumnOf(b, &m.Maybe, orm.Codec(c))}
	})
	must(t, orm.Check(fields))
	_, err := db.Exec(t.Context(), "CREATE TABLE codec_"+name+" (id bigint PRIMARY KEY, value "+sqlType+" NOT NULL, maybe "+sqlType+")")
	must(t, err)
	_, err = fields.Insert(t.Context(), db, fields.Key().Set(1), fields.Value.Set(input), fields.Maybe.SetNull())
	must(t, err)
	got, err := fields.Require(t.Context(), db, 1)
	must(t, err)
	if !reflect.DeepEqual(got.Value, input) || !got.Maybe.IsNull() {
		t.Fatalf("%s required/null round trip", name)
	}
	_, err = fields.Update(t.Context(), db, 1, fields.Maybe.Set(input))
	must(t, err)
	got, err = fields.Require(t.Context(), db, 1)
	must(t, err)
	nullable, ok := got.Maybe.Get()
	if !ok || !reflect.DeepEqual(nullable, input) {
		t.Fatalf("%s nullable value round trip", name)
	}
}

func TestColumnKinds(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	roundTripKind(t, db, "bool", "boolean", true, codec.Bool[bool]())
	roundTripKind(t, db, "text", "text", "MiXeD_%\\", codec.String[string]())
	roundTripKind(t, db, "int", "bigint", int(-17), codec.Signed[int]())
	roundTripKind(t, db, "int8", "smallint", int8(-127), codec.Signed[int8]())
	roundTripKind(t, db, "int16", "smallint", int16(-32767), codec.Signed[int16]())
	roundTripKind(t, db, "int32", "integer", int32(-2147483647), codec.Signed[int32]())
	roundTripKind(t, db, "int64", "bigint", int64(-9223372036854775807), codec.Signed[int64]())
	roundTripKind(t, db, "uint", "bigint", uint(17), codec.Unsigned[uint]())
	roundTripKind(t, db, "uint8", "smallint", uint8(255), codec.Unsigned[uint8]())
	roundTripKind(t, db, "uint16", "integer", uint16(65535), codec.Unsigned[uint16]())
	roundTripKind(t, db, "uint32", "bigint", uint32(4294967295), codec.Unsigned[uint32]())
	roundTripKind(t, db, "uint64", "bigint", uint64(9223372036854775807), codec.Unsigned[uint64]())
	roundTripKind(t, db, "float32", "real", float32(1.25), codec.Float[float32]())
	roundTripKind(t, db, "float64", "double precision", float64(-1.25), codec.Float[float64]())
	roundTripKind(t, db, "decimal", "numeric", decimal.FromInt64(123), codec.Decimal())
	instant := time.Date(2026, 10, 23, 12, 34, 56, 123000000, time.UTC)
	roundTripKind(t, db, "instant", "timestamptz", instant, codec.Time())
	dt, err := temporal.NewDateTime(instant)
	must(t, err)
	roundTripKind(t, db, "datetime", "timestamptz", dt, codec.DateTime())
	date, err := temporal.ParseDate("2026-10-23")
	must(t, err)
	roundTripKind(t, db, "date", "date", date, codec.Date())
	wall, err := temporal.ParseTime("12:34:56.123")
	must(t, err)
	roundTripKind(t, db, "time", "time", wall, codec.WallTime())
	local, err := temporal.ParseLocalDateTime("2026-10-23T12:34:56.123")
	must(t, err)
	roundTripKind(t, db, "local", "timestamp", local, codec.LocalDateTime())
	id, err := model.NewID[models.User]()
	must(t, err)
	roundTripKind(t, db, "id", "uuid", id, codec.ID[models.User]())
	roundTripKind(t, db, "enum", "text", models.StatusActive, codec.String[models.Status]().Validated(models.Status.Validate))
	// JSON null is a value, distinct from SQL NULL at the nullable boundary.
	jsonNull, err := value.NewJSON(value.Null[string]())
	must(t, err)
	roundTripKind(t, db, "json_null", "jsonb", jsonNull, codec.JSON[value.Nullable[string]]())
}

func TestJSONIntervalBinaryAndNaturalKeys(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE core_records (code text PRIMARY KEY, settings jsonb NOT NULL, maybe_settings jsonb, period interval NOT NULL, maybe_period interval, body bytea NOT NULL, maybe_body bytea, foreign_id uuid NOT NULL, revision uuid NOT NULL, message_id uuid NOT NULL, status text NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE TABLE core_durations (period interval PRIMARY KEY, label text NOT NULL)`)
	must(t, err)
	must(t, orm.Check(coremodels.Records, coremodels.Durations))
	r := coremodels.Records
	settings, err := value.NewJSON(coremodels.Preferences{Theme: "dark"})
	must(t, err)
	period, err := temporal.NewInterval(2, -3, 4*time.Hour+5*time.Microsecond)
	must(t, err)
	input := coremodels.Payload{1, 2, 3}
	body := r.Body.Set(input)
	input[0] = 9 // Pending assignments must own binary buffers.
	_, err = r.Insert(ctx, db, r.Code.Set("sample"), r.Settings.Set(settings), r.Period.Set(period), body, r.ForeignID.Set(models.User{}.ID), r.Revision.Set(coremodels.Record{}.Revision), r.MessageID.Set(coremodels.Record{}.MessageID), r.Status.Set(models.StatusActive))
	must(t, err)
	got, err := r.Require(ctx, db, "sample")
	must(t, err)
	if got.Settings != settings || got.Period != period || !bytes.Equal(got.Body, []byte{1, 2, 3}) || !got.MaybeSettings.IsNull() || !got.MaybePeriod.IsNull() || !got.MaybeBody.IsNull() {
		t.Fatal("new constructor round trip/null/ownership")
	}
	_, err = r.Update(ctx, db, "sample", r.MaybeSettings.Set(settings), r.MaybePeriod.Set(period), r.MaybeBody.Set(coremodels.Payload{}))
	must(t, err)
	got, err = r.Require(ctx, db, "sample")
	must(t, err)
	js, jok := got.MaybeSettings.Get()
	iv, iok := got.MaybePeriod.Get()
	binary, bok := got.MaybeBody.Get()
	if !jok || js != settings || !iok || iv != period || !bok || binary == nil || len(binary) != 0 {
		t.Fatal("nullable value/empty binary round trip")
	}
	_, err = coremodels.Durations.Insert(ctx, db, coremodels.Durations.Period.Set(period), coremodels.Durations.Label.Set(`Cal_%\endar`))
	must(t, err)
	duration, err := coremodels.Durations.Require(ctx, db, period)
	must(t, err)
	if duration.Label != `Cal_%\endar` || duration.Period != period {
		t.Fatal("interval key codec")
	}
	matches, err := coremodels.Durations.Query().Where(coremodels.Durations.Label.IContains(`_%\E`)).Count(ctx, db)
	must(t, err)
	if matches != 1 {
		t.Fatal("literal case-insensitive substring")
	}
	matching, err := r.Query().Where(r.Period.Gt(temporal.Days(1)), r.Settings.Eq(settings), r.Settings.Contains(settings), r.MaybeSettings.ContainedBy(settings)).All(ctx, db)
	must(t, err)
	if len(matching) != 1 {
		t.Fatal("interval ordering/JSON equality")
	}
	if _, err = r.Update(ctx, db, "sample", r.Status.Set(models.Status("invalid"))); err == nil {
		t.Fatal("invalid enum accepted")
	}
	stored, err := r.Require(ctx, db, "sample")
	must(t, err)
	if stored.Status != models.StatusActive {
		t.Fatal("invalid enum changed persisted value")
	}
	for _, bad := range []struct{ name, sql string }{
		{"json", `UPDATE core_records SET settings='{"theme":42}'::jsonb WHERE code='sample'`},
		{"enum", `UPDATE core_records SET settings='{"theme":"dark"}'::jsonb,status='invalid' WHERE code='sample'`},
	} {
		t.Run(bad.name, func(t *testing.T) {
			_, err := db.Exec(ctx, bad.sql)
			must(t, err)
			got, err := r.Query().Where(r.Code.Eq("sample")).All(ctx, db)
			if err == nil || got != nil {
				t.Fatal("malformed stored value published a model")
			}
		})
	}
}

func TestJSONOrderingAndNullPredicate(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	type record struct {
		ID       int64
		Document value.JSON[value.Nullable[string]]
		Maybe    value.Nullable[value.JSON[value.Nullable[string]]]
	}
	type fields struct {
		orm.Table[record, int64]
		Document orm.JSON[record, value.Nullable[string]]
		Maybe    orm.NullableJSON[record, value.Nullable[string]]
	}
	r := orm.Define("json_order_null", func(b orm.Builder[record], m *record) fields {
		return fields{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Document), orm.NullableJSONOf(b, &m.Maybe)}
	})
	_, err := db.Exec(ctx, `CREATE TABLE json_order_null (id bigint PRIMARY KEY, document jsonb NOT NULL, maybe jsonb)`)
	must(t, err)
	must(t, orm.Check(r))
	null, err := value.NewJSON(value.Null[string]())
	must(t, err)
	text, err := value.NewJSON(value.Of("text"))
	must(t, err)
	_, err = r.Insert(ctx, db, r.Key().Set(1), r.Document.Set(null), r.Maybe.SetNull())
	must(t, err)
	_, err = r.Insert(ctx, db, r.Key().Set(2), r.Document.Set(text), r.Maybe.Set(null))
	must(t, err)
	for _, check := range []struct {
		predicate orm.Predicate[record]
		id        int64
	}{
		{r.Document.IsJSONNull(), 1}, {r.Maybe.IsJSONNull(), 2}, {r.Maybe.IsNull(), 1},
	} {
		got, err := r.Query().Where(check.predicate).Require(ctx, db)
		must(t, err)
		if got.ID != check.id {
			t.Fatalf("JSON null/SQL NULL selected %d, want %d", got.ID, check.id)
		}
	}
	for _, check := range []struct {
		order orm.Order[record]
		first int64
	}{
		{r.Document.Asc(), 1}, {r.Document.Desc(), 2}, {r.Maybe.Asc(), 2}, {r.Maybe.Desc(), 1},
	} {
		got, err := r.Query().OrderBy(check.order).All(ctx, db)
		must(t, err)
		if len(got) != 2 || got[0].ID != check.first {
			t.Fatal("JSON sort order")
		}
	}
}

func TestIntervalComparisonSemantics(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	type record struct {
		ID     int64
		Period temporal.Interval
		Maybe  value.Nullable[temporal.Interval]
	}
	type fields struct {
		orm.Table[record, int64]
		Period orm.Interval[record]
		Maybe  orm.NullableInterval[record]
	}
	r := orm.Define("interval_comparison", func(b orm.Builder[record], m *record) fields {
		return fields{orm.TableOf(b, &m.ID), orm.IntervalOf(b, &m.Period), orm.NullableIntervalOf(b, &m.Maybe)}
	})
	_, err := db.Exec(t.Context(), `CREATE TABLE interval_comparison (id bigint PRIMARY KEY, period interval NOT NULL, maybe interval)`)
	must(t, err)
	_, err = r.Insert(t.Context(), db, r.Key().Set(1), r.Period.Set(temporal.Days(30)), r.Maybe.Set(temporal.Days(30)))
	must(t, err)
	got, err := r.Query().Where(r.Period.Eq(temporal.Months(1)), r.Maybe.Eq(temporal.Months(1))).Require(t.Context(), db)
	must(t, err)
	if got.ID != 1 || got.Period != temporal.Days(30) {
		t.Fatal("month equality must match 30 days without changing hydrated components")
	}
}
