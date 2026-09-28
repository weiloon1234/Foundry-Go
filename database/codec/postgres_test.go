package codec_test

import (
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

func bind[T any](t *testing.T, c codec.Codec[T], v T) any {
	t.Helper()
	encoded, err := c.Bind(v)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestPostgresTypedCodecRoundTrips(t *testing.T) {
	db := pgtest.Open(t)
	id, err := model.NewID[owner]()
	if err != nil {
		t.Fatal(err)
	}
	amount, err := decimal.Parse("123456789012345678901234567890.12345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	instant, _ := temporal.ParseDateTime("2026-09-11T12:34:56.123456+08:00")
	date, _ := temporal.ParseDate("2024-02-29")
	wall, _ := temporal.ParseTime("12:34:56.123456")
	local, _ := temporal.ParseLocalDateTime("2026-09-11T12:34:56.123456")
	var actualID model.ID[owner]
	var actualAmount decimal.Decimal
	var actualInstant temporal.DateTime
	var actualDate temporal.Date
	var actualWall temporal.Time
	var actualLocal temporal.LocalDateTime
	var actualNull value.Nullable[decimal.Decimal]
	var actualText code
	var actualSmall small
	var actualUnsigned uint16
	var actualBool bool
	var actualFloat float32
	var actualTime time.Time
	var actualZero value.Nullable[int64]
	args := []any{
		bind(t, codec.ID[owner](), id), bind(t, codec.Decimal(), amount),
		bind(t, codec.DateTime(), instant), bind(t, codec.Date(), date),
		bind(t, codec.WallTime(), wall), bind(t, codec.LocalDateTime(), local),
		bind(t, codec.Nullable(codec.Decimal()), value.Null[decimal.Decimal]()),
		bind(t, codec.String[code](), code("SQL-looking ' -- unicode ☺")), bind(t, codec.Signed[small](), small(-128)),
		bind(t, codec.Unsigned[uint16](), uint16(65535)), bind(t, codec.Bool[bool](), true),
		bind(t, codec.Float[float32](), float32(1.25)), bind(t, codec.Time(), instant.UTC()),
		bind(t, codec.Nullable(codec.Signed[int64]()), value.Of(int64(0))),
	}
	err = database.ScanOne(t.Context(), db,
		"SELECT $1::uuid,$2::numeric,$3::timestamptz,$4::date,$5::time,$6::timestamp,$7::numeric,$8::text,$9::smallint,$10::integer,$11::boolean,$12::real,$13::timestamptz,$14::bigint", args,
		codec.ID[owner]().Scan(&actualID), codec.Decimal().Scan(&actualAmount),
		codec.DateTime().Scan(&actualInstant), codec.Date().Scan(&actualDate),
		codec.WallTime().Scan(&actualWall), codec.LocalDateTime().Scan(&actualLocal),
		codec.Nullable(codec.Decimal()).Scan(&actualNull), codec.String[code]().Scan(&actualText),
		codec.Signed[small]().Scan(&actualSmall), codec.Unsigned[uint16]().Scan(&actualUnsigned),
		codec.Bool[bool]().Scan(&actualBool), codec.Float[float32]().Scan(&actualFloat), codec.Time().Scan(&actualTime),
		codec.Nullable(codec.Signed[int64]()).Scan(&actualZero))
	if err != nil {
		t.Fatal(err)
	}
	if actualID != id || actualAmount != amount || actualInstant != instant || actualDate != date || actualWall != wall || actualLocal != local || !actualNull.IsNull() || actualText != "SQL-looking ' -- unicode ☺" || actualSmall != -128 || actualUnsigned != 65535 || !actualBool || actualFloat != 1.25 || !actualTime.Equal(instant.UTC()) || actualZero != value.Of(int64(0)) {
		t.Fatal("PostgreSQL typed codec round trip changed a value or its NULL state")
	}
}

func TestPostgresMalformedValuesNeverReplaceCodecDestinations(t *testing.T) {
	db := pgtest.Open(t)
	for _, statement := range []string{"SELECT 'NaN'::numeric", "SELECT 'Infinity'::numeric", "SELECT 1.25::double precision", "SELECT NULL::numeric"} {
		current := decimal.FromInt64(42)
		if err := database.ScanOne(t.Context(), db, statement, nil, codec.Decimal().Scan(&current)); err == nil || current != decimal.FromInt64(42) {
			t.Fatal("invalid exact value accepted or replaced destination")
		}
	}
	current := small(12)
	if err := database.ScanOne(t.Context(), db, "SELECT 128::integer", nil, codec.Signed[small]().Scan(&current)); err == nil || current != 12 {
		t.Fatal("stored overflow accepted or replaced destination")
	}
	if err := db.Ping(t.Context()); err != nil {
		t.Fatal("failed scanning did not release pool ownership")
	}
}
