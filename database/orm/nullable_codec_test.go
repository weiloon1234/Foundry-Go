package orm_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type nullableOwner struct{}
type nullableUserID model.ID[nullableOwner]
type nullableUnsupported struct{ X int }
type nullableModel[V any] struct {
	ID  int
	Bad value.Nullable[V]
}
type nullableTable[V any] struct {
	orm.Table[nullableModel[V], int]
	Bad orm.NullableColumn[nullableModel[V], V]
}
type nullableRow[V any] struct{ Bad value.Nullable[V] }
type nullableFields[V any] struct {
	rows.Row[nullableRow[V]]
	Bad rows.Field[nullableRow[V], value.Nullable[V]]
}

// An empty result is intentional: decoding must not be needed to reject the codec.
type emptyNullableDriver struct{ lazyDriver }

func (d *emptyNullableDriver) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (d *emptyNullableDriver) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	d.queries++
	return &lazyRows{done: true}, nil
}

func checkNullableCodec[V any](t *testing.T, options ...orm.ColumnOption[V]) {
	t.Helper()
	table := orm.Define("invalid_nullable", func(b orm.Builder[nullableModel[V]], m *nullableModel[V]) nullableTable[V] {
		return nullableTable[V]{orm.TableOf(b, &m.ID), orm.NullableColumnOf(b, &m.Bad, options...)}
	})
	d := &emptyNullableDriver{}
	db, err := database.Open(t.Context(), database.Adapter{Connector: d}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	invalid := func(err error) {
		t.Helper()
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "Bad") {
			t.Fatalf("want invalid Bad codec, got %v", err)
		}
		if d.queries != 0 {
			t.Fatalf("invalid declaration sent %d statements", d.queries)
		}
	}
	invalid(orm.Check(table))
	_, err = table.Query().Count(t.Context(), db)
	invalid(err)
	_, err = table.Query().Exists(t.Context(), db)
	invalid(err)
	_, err = table.Query().All(t.Context(), db)
	invalid(err)
	_, err = rows.Read(rows.SelectValue(table.Query(), table.Key())).All(t.Context(), db)
	invalid(err)

	// A valid source isolates row validation from table validation. The invalid field
	// is mapped so this cannot pass merely because the row is incompletely selected.
	valid := codec.New(func(V) (driver.Value, error) { return "value", nil }, func(any) (V, error) { return *new(V), nil })
	source := orm.Define("valid_nullable", func(b orm.Builder[nullableModel[V]], m *nullableModel[V]) nullableTable[V] {
		return nullableTable[V]{orm.TableOf(b, &m.ID), orm.NullableColumnOf(b, &m.Bad, orm.Codec(valid))}
	})
	if err := orm.Check(source); err != nil {
		t.Fatal(err)
	}
	option := orm.ColumnOption[V]{}
	if len(options) > 0 {
		option = options[0]
	}
	schema := rows.Define(func(b rows.Builder[nullableRow[V]], r *nullableRow[V]) nullableFields[V] {
		return nullableFields[V]{b.Row(), rows.NullableFieldOf(b, &r.Bad, option)}
	})
	invalid(rows.Check(schema))
	alias := rows.As[struct{}](source.Query(), "valid")
	_, err = rows.Read(rows.Select(alias, schema, rows.Map(schema.Bad, expr.At(rows.Scope(alias), source.Bad)))).All(t.Context(), db)
	invalid(err)
}

func TestNullableInnerCodecRejectedBeforeSQL(t *testing.T) {
	t.Run("unsupported_struct", func(t *testing.T) { checkNullableCodec[nullableUnsupported](t) })
	t.Run("defined_id_without_codec", func(t *testing.T) { checkNullableCodec[nullableUserID](t) })
	t.Run("unsupported_bytes_fallback", func(t *testing.T) { checkNullableCodec[[]byte](t) })
	t.Run("empty_option", func(t *testing.T) { checkNullableCodec(t, orm.ColumnOption[int]{}) })
	t.Run("invalid_codec", func(t *testing.T) { checkNullableCodec(t, orm.Codec(codec.Codec[int]{})) })
	t.Run("missing_decoder", func(t *testing.T) {
		checkNullableCodec(t, orm.Codec(codec.New(func(v int) (driver.Value, error) { return int64(v), nil }, nil)))
	})
	t.Run("missing_encoder", func(t *testing.T) {
		checkNullableCodec(t, orm.Codec(codec.New[int](nil, func(any) (int, error) { return 0, nil })))
	})
}
