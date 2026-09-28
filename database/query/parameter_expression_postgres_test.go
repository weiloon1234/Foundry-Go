package query

import (
	"database/sql/driver"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func parameterRoundTrip[V any](t *testing.T, tx *database.Tx, c codec.Codec[V], input, want V) {
	t.Helper()
	actual, err := SelectValue(cursorQuery(), parameterExpression[cursorRecord](input, c).Value()).RequireFirst(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(actual, want) {
		t.Fatalf("parameter %T: got %v, want %v: %v", want, actual, want, err)
	}
}

func TestPostgresStandaloneParameterTypes(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `CREATE TABLE records (id bigint PRIMARY KEY, rank bigint)`, `INSERT INTO records (id) VALUES (1)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		id, err := model.NewID[cursorRecord]()
		if err != nil {
			return err
		}
		amount, err := decimal.Parse("12345678901234567890.123456789")
		if err != nil {
			return err
		}
		date, err := temporal.ParseDate("2024-02-29")
		if err != nil {
			return err
		}
		wall, err := temporal.ParseTime("12:34:56.123456")
		if err != nil {
			return err
		}
		local, err := temporal.ParseLocalDateTime("2024-02-29T12:34:56.123456")
		if err != nil {
			return err
		}
		instant, err := temporal.ParseDateTime("2024-02-29T12:34:56.123456+08:00")
		if err != nil {
			return err
		}
		parameterRoundTrip(t, tx, codec.Bool[bool](), true, true)
		parameterRoundTrip(t, tx, codec.Signed[int8](), int8(-7), int8(-7))
		parameterRoundTrip(t, tx, codec.Unsigned[uint64](), uint64(math.MaxInt64), uint64(math.MaxInt64))
		parameterRoundTrip(t, tx, codec.Float[float32](), float32(0.1), float32(0.1))
		parameterRoundTrip(t, tx, codec.Float[float64](), 1.2345, 1.2345)
		parameterRoundTrip(t, tx, codec.String[string](), "' SQL? 世界", "' SQL? 世界")
		parameterRoundTrip(t, tx, codec.ID[cursorRecord](), id, id)
		parameterRoundTrip(t, tx, codec.Decimal(), amount, amount)
		parameterRoundTrip(t, tx, codec.Date(), date, date)
		parameterRoundTrip(t, tx, codec.WallTime(), wall, wall)
		parameterRoundTrip(t, tx, codec.LocalDateTime(), local, local)
		parameterRoundTrip(t, tx, codec.DateTime(), instant, instant)
		utc := time.Date(2024, 2, 29, 4, 34, 56, 123456000, time.UTC)
		parameterRoundTrip(t, tx, codec.Time(), utc.In(time.FixedZone("eight", 8*3600)), utc)
		parameterRoundTrip(t, tx, codec.Nullable(codec.Decimal()), value.Null[decimal.Decimal](), value.Null[decimal.Decimal]())
		parameterRoundTrip(t, tx, codec.Nullable(codec.ID[cursorRecord]()), value.Of(id), value.Of(id))
		binary := codec.New(func(v [2]byte) (driver.Value, error) { return v[:], nil }, func(source any) ([2]byte, error) {
			bytes, ok := source.([]byte)
			if !ok || len(bytes) != 2 {
				return [2]byte{}, fault.New(fault.Invalid, "unexpected binary result")
			}
			return [2]byte{bytes[0], bytes[1]}, nil
		}).WithParameterType(codec.TypeBytes)
		parameterRoundTrip(t, tx, binary, [2]byte{0, 255}, [2]byte{0, 255})
		buffer := codec.New(func(v []byte) (driver.Value, error) { return v, nil }, func(source any) ([]byte, error) {
			data, ok := source.([]byte)
			if !ok {
				return nil, fault.New(fault.Invalid, "unexpected binary buffer")
			}
			return slices.Clone(data), nil
		}).WithParameterType(codec.TypeBytes)
		for _, input := range []value.Nullable[[]byte]{value.Of([]byte{}), value.Of([]byte{0, 255}), value.Null[[]byte]()} {
			parameterRoundTrip(t, tx, codec.Nullable(buffer), input, input)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
