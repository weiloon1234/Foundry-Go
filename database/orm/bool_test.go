package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

type booleanFlag bool
type booleanModel struct {
	ID      int64
	Enabled booleanFlag
}
type booleanFields struct {
	Table[booleanModel, int64]
	Enabled Column[booleanModel, booleanFlag]
}

func TestBoolOfPreservesCodecAndFalse(t *testing.T) {
	fields := Define("boolean_models", func(b Builder[booleanModel], m *booleanModel) booleanFields {
		return booleanFields{TableOf(b, &m.ID), BoolOf(b, &m.Enabled)}
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	draft := &Write[booleanModel]{state: &draftState{table: fields.Table.core, open: true}}
	if err := WriteSet(draft, fields.Enabled.Set(false)); err != nil {
		t.Fatal(err)
	}
	input, present := WriteGet(draft, fields.Enabled.Set).Get()
	if !present || bool(input) {
		t.Fatal("false assignment lost presence/value")
	}
	statement, err := Compile(fields.Query().Where(fields.Enabled.Eq(false)))
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{false}) {
		t.Fatal("false predicate lost", err)
	}
	db := testDB(t, testConnector{columns: []string{"id", "enabled"}, values: [][]driver.Value{{int64(1), false}}})
	got, err := fields.Query().All(t.Context(), db)
	if err != nil || len(got) != 1 || got[0].Enabled {
		t.Fatal("false hydration changed", err)
	}
	rejected := errors.New("Boolean codec rejected false")
	custom := Define("custom_boolean_models", func(b Builder[booleanModel], m *booleanModel) booleanFields {
		return booleanFields{TableOf(b, &m.ID), BoolOf(b, &m.Enabled, Codec(codec.Bool[booleanFlag]().Validated(func(v booleanFlag) error {
			if !v {
				return rejected
			}
			return nil
		})))}
	})
	if err := Check(custom); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(custom.Query().Where(custom.Enabled.Eq(false))); !errors.Is(err, rejected) {
		t.Fatal("custom Boolean codec was ignored", err)
	}
	mutated := Define("mutated_boolean_models", func(b Builder[booleanModel], m *booleanModel) booleanFields {
		return booleanFields{TableOf(b, &m.ID), BoolOf(b, &m.Enabled, Mutate(func(v booleanFlag) (booleanFlag, error) { return v, nil }))}
	})
	if err := Check(mutated); err == nil {
		t.Fatal("ordinary bool carrier accepted mutator")
	}
	duplicate := Define("duplicate_boolean_models", func(b Builder[booleanModel], m *booleanModel) booleanFields {
		return booleanFields{TableOf(b, &m.ID), BoolOf(b, &m.Enabled, Codec(codec.Bool[booleanFlag]()), Codec(codec.Bool[booleanFlag]()))}
	})
	if err := Check(duplicate); err == nil {
		t.Fatal("duplicate codecs accepted")
	}
}
