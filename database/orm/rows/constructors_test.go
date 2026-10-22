package rows

import (
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cell[V any] struct{ Value V }
type namedText string
type namedNumber int64
type rowStatus string

func (rowStatus) EnumDescriptor() enum.Descriptor[rowStatus] {
	return enum.Describe("rows_test", "RowStatus", enum.Case[rowStatus]{Name: "Active", Value: "active"})
}

type badStatus string

func (badStatus) EnumDescriptor() enum.Descriptor[badStatus] { return enum.Descriptor[badStatus]{} }

type customValue struct{ Text string }

// Use database.Rows and the same offset/codec decoder used by projection.All.
func hydrateCell[V any](t *testing.T, declare func(Builder[cell[V]], *V) Field[cell[V], V], input driver.Value) (V, error) {
	t.Helper()
	schema := Define(func(b Builder[cell[V]], r *cell[V]) Row[cell[V]] {
		declare(b, &r.Value)
		return b.Row()
	})
	if err := Check(schema); err != nil {
		t.Fatal(err)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{input}}}
	db := openTest(t, state)
	rows, err := db.Query(t.Context(), "SELECT value")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing canned row")
	}
	got, err := schema.decode(rows)
	return got.Value, err
}
func checkCell[V any](t *testing.T, declare func(Builder[cell[V]], *V) Field[cell[V], V], input driver.Value, want V) {
	t.Helper()
	got, err := hydrateCell(t, declare, input)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%T: got %#v, want %#v; %v", want, got, want, err)
	}
}
func TestRowKindConstructorsHydrate(t *testing.T) {
	id, err := model.ParseID[member]("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	amount, err := decimal.Parse("12345678901234567890.001")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC)
	instant, err := temporal.NewDateTime(now)
	if err != nil {
		t.Fatal(err)
	}
	date, err := temporal.ParseDate("2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	wall, err := temporal.ParseTime("01:02:03")
	if err != nil {
		t.Fatal(err)
	}
	local, err := temporal.ParseLocalDateTime("2026-10-22T01:02:03")
	if err != nil {
		t.Fatal(err)
	}
	checkCell(t, IDOf[cell[model.ID[member]], member], id.String(), id)
	checkCell(t, TextOf[cell[namedText], namedText], "hello", namedText("hello"))
	checkCell(t, NumberOf[cell[namedNumber], namedNumber], int64(42), namedNumber(42))
	checkCell(t, NumberOf[cell[uint64], uint64], int64(42), uint64(42))
	checkCell(t, NumberOf[cell[float64], float64], float64(1.25), 1.25)
	checkCell(t, NumberOf[cell[decimal.Decimal], decimal.Decimal], amount.String(), amount)
	checkCell(t, EnumOf[cell[rowStatus], rowStatus], "active", rowStatus("active"))
	checkCell(t, TimeOf[cell[time.Time], time.Time], now, now)
	checkCell(t, TimeOf[cell[temporal.DateTime], temporal.DateTime], now, instant)
	checkCell(t, TimeOf[cell[temporal.Date], temporal.Date], "2026-10-22", date)
	checkCell(t, TimeOf[cell[temporal.Time], temporal.Time], "01:02:03", wall)
	checkCell(t, TimeOf[cell[temporal.LocalDateTime], temporal.LocalDateTime], "2026-10-22T01:02:03", local)
	bytes := []byte{1, 2, 3}
	got, err := hydrateCell(t, BytesOf[cell[[]byte], []byte], bytes)
	if err != nil || !reflect.DeepEqual(got, bytes) {
		t.Fatal(got, err)
	}
	bytes[0] = 9
	if got[0] != 1 {
		t.Fatal("row retained driver byte buffer")
	}
	checkCell(t, NullableIDOf[cell[value.Nullable[model.ID[member]]], member], id.String(), value.Of(id))
	checkCell(t, NullableTextOf[cell[value.Nullable[namedText]], namedText], "hello", value.Of(namedText("hello")))
	checkCell(t, NullableNumberOf[cell[value.Nullable[int64]], int64], int64(42), value.Of(int64(42)))
	checkCell(t, NullableNumberOf[cell[value.Nullable[decimal.Decimal]], decimal.Decimal], amount.String(), value.Of(amount))
	checkCell(t, NullableEnumOf[cell[value.Nullable[rowStatus]], rowStatus], "active", value.Of(rowStatus("active")))
	checkCell(t, NullableTimeOf[cell[value.Nullable[temporal.DateTime]], temporal.DateTime], now, value.Of(instant))
	checkCell(t, NullableBytesOf[cell[value.Nullable[[]byte]], []byte], []byte{1, 2}, value.Of([]byte{1, 2}))
	checkCell(t, NullableIDOf[cell[value.Nullable[model.ID[member]]], member], nil, value.Null[model.ID[member]]())
	checkCell(t, NullableTextOf[cell[value.Nullable[namedText]], namedText], nil, value.Null[namedText]())
	checkCell(t, NullableNumberOf[cell[value.Nullable[int64]], int64], nil, value.Null[int64]())
	checkCell(t, NullableEnumOf[cell[value.Nullable[rowStatus]], rowStatus], nil, value.Null[rowStatus]())
	checkCell(t, NullableTimeOf[cell[value.Nullable[temporal.DateTime]], temporal.DateTime], nil, value.Null[temporal.DateTime]())
	checkCell(t, NullableBytesOf[cell[value.Nullable[[]byte]], []byte], nil, value.Null[[]byte]())
}
func TestRowEnumRejectsInvalidDescriptorAndValues(t *testing.T) {
	schema := Define(func(b Builder[cell[badStatus]], r *cell[badStatus]) Row[cell[badStatus]] {
		EnumOf(b, &r.Value)
		return b.Row()
	})
	if err := Check(schema); err == nil {
		t.Fatal("invalid descriptor accepted")
	}
	if _, err := hydrateCell(t, EnumOf[cell[rowStatus], rowStatus], "unknown"); err == nil {
		t.Fatal("unknown enum hydrated")
	}
	if _, err := hydrateCell(t, NullableEnumOf[cell[value.Nullable[rowStatus]], rowStatus], "unknown"); err == nil {
		t.Fatal("unknown nullable enum hydrated")
	}
}
func TestRowCustomCodecOption(t *testing.T) {
	custom := codec.Adapt(codec.String[string](), func(v customValue) string { return v.Text }, func(v string) customValue { return customValue{v} })
	checkCell(t, func(b Builder[cell[customValue]], v *customValue) Field[cell[customValue], customValue] {
		return FieldOf(b, v, orm.Codec(custom))
	}, "custom", customValue{"custom"})
	nullable := func(b Builder[cell[value.Nullable[customValue]]], v *value.Nullable[customValue]) Field[cell[value.Nullable[customValue]], value.Nullable[customValue]] {
		return NullableFieldOf(b, v, orm.Codec(custom))
	}
	checkCell(t, nullable, "custom", value.Of(customValue{"custom"}))
	checkCell(t, nullable, nil, value.Null[customValue]())
	bad := Define(func(b Builder[cell[customValue]], r *cell[customValue]) Row[cell[customValue]] {
		FieldOf(b, &r.Value, orm.ColumnOption[customValue]{})
		return b.Row()
	})
	if err := Check(bad); err == nil || !strings.Contains(err.Error(), "orm.Codec") {
		t.Fatalf("missing codec advice: %v", err)
	}
}
