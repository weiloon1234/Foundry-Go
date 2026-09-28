package codec_test

import (
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCustomCodecPreservesSQLNullAndEmptyBytes(t *testing.T) {
	c := codec.New(func(input driver.Value) (driver.Value, error) { return input, nil }, func(raw any) (driver.Value, error) { return raw, nil }).WithParameterType(codec.TypeBytes)
	for _, input := range []driver.Value{nil, []byte(nil)} {
		bound, err := c.Bind(input)
		if err != nil || bound != nil {
			t.Fatal("custom codec did not normalize SQL NULL", err)
		}
	}
	bound, err := c.Bind([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	empty, ok := bound.([]byte)
	if !ok || empty == nil || len(empty) != 0 {
		t.Fatal("empty bytes became SQL NULL")
	}
	change, err := lifecycle.CompareField(c, value.Set[driver.Value](nil), value.Set[driver.Value]([]byte(nil)), true)
	if err != nil || change.Changed() || !change.Assigned() {
		t.Fatal("equivalent SQL NULL encodings became a stored change", err)
	}
	change, err = lifecycle.CompareField(c, value.Set[driver.Value]([]byte(nil)), value.Set[driver.Value]([]byte{}), true)
	if err != nil || !change.Changed() {
		t.Fatal("NULL-to-empty byte change was lost", err)
	}
}
