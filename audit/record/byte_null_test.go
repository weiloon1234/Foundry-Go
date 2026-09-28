package record_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestAuditByteNullIsDistinctFromEmptyAndAbsentModel(t *testing.T) {
	c := codec.New(func(v buffer) (driver.Value, error) { return v.data, nil }, func(raw any) (buffer, error) {
		if raw == nil {
			return buffer{}, nil
		}
		data, ok := raw.([]byte)
		if !ok {
			return buffer{}, errors.New("expected byte value")
		}
		return buffer{data: data}, nil
	}).WithParameterType(codec.TypeBytes)
	b := builder(t, lifecycle.Update)
	change := changed(t, c, value.Set(buffer{}), value.Set(buffer{data: []byte{}}), true)
	if err := b.Add(capture(t, "data", c, change, record.Automatic)); err != nil {
		t.Fatal(err)
	}
	view := inspect(t, built(t, b))
	field, err := record.ReadField(view, "data", c)
	if err != nil {
		t.Fatal(err)
	}
	stored, present := field.Get()
	if !present || !stored.Changed() {
		t.Fatal("byte change omitted")
	}
	before, err := stored.Before().Get()
	if err != nil {
		t.Fatal(err)
	}
	left, present := before.Get()
	if !present || left.data != nil || stored.Before().State() != record.Disclosed {
		t.Fatal("SQL NULL became empty bytes or an absent model")
	}
	after, err := stored.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	right, present := after.Get()
	if !present || right.data == nil || len(right.data) != 0 {
		t.Fatal("empty bytes became SQL NULL")
	}
}
