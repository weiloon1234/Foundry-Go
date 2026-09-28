package codec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type binaryBlob []byte

func TestBytesOwnBuffersAndDistinguishNull(t *testing.T) {
	c := codec.Bytes[binaryBlob]()
	input := binaryBlob{0, 255, 1}
	bound, err := c.Bind(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 9
	if !bytes.Equal(bound.([]byte), []byte{0, 255, 1}) {
		t.Fatal("binding aliased its input")
	}
	decoded, err := c.Decode(bound)
	if err != nil {
		t.Fatal(err)
	}
	bound.([]byte)[1] = 0
	if !bytes.Equal(decoded, []byte{0, 255, 1}) {
		t.Fatal("hydration retained driver storage")
	}
	for _, raw := range []any{nil, []byte(nil), "text", 1, binaryBlob{1}} {
		if _, err := c.Decode(raw); !errors.Is(err, fault.Invalid) {
			t.Fatalf("accepted non-driver bytes %T: %v", raw, err)
		}
	}
	if _, err := c.Bind(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil non-null value accepted", err)
	}
	empty, err := c.Bind(binaryBlob{})
	if err != nil || empty == nil || empty.([]byte) == nil || len(empty.([]byte)) != 0 {
		t.Fatal("empty bytes became NULL", err)
	}
	nullable := codec.Nullable(c)
	null, err := nullable.Bind(value.Null[binaryBlob]())
	if err != nil || null != nil {
		t.Fatal("explicit NULL did not bind", err)
	}
	if _, err := nullable.Bind(value.Of[binaryBlob](nil)); !errors.Is(err, fault.Invalid) {
		t.Fatal("present nil bypassed binary validation", err)
	}
	decodedNull, err := nullable.Decode(nil)
	if err != nil || !decodedNull.IsNull() {
		t.Fatal("SQL NULL did not hydrate", err)
	}
}

func TestBinaryOwnershipSurvivesAdaptersAndChangeSnapshots(t *testing.T) {
	calls := 0
	c := codec.Bytes[binaryBlob]().Validated(func(binaryBlob) error { calls++; return nil }).WithSensitiveValues().WithParameterType(codec.TypeBytes)
	input := binaryBlob{1, 2}
	nullable := codec.Nullable(c)
	owned := nullable.CloneOptional(value.Set(value.Of(input)))
	if calls != 0 || !nullable.SensitiveValues() || nullable.ParameterType() != codec.TypeBytes {
		t.Fatal("ownership invoked validation or lost metadata")
	}
	input[0] = 9
	v, _ := owned.Get()
	data, _ := v.Get()
	if data[0] != 1 {
		t.Fatal("nullable snapshot retained its input")
	}
	before, after := binaryBlob{3}, binaryBlob{4}
	change, err := lifecycle.CompareField(c, value.Set(before), value.Set(after), true)
	if err != nil || !change.Changed() || !change.Assigned() {
		t.Fatal("binary comparison failed", err)
	}
	before[0], after[0] = 8, 8
	left, _ := change.Before().Get()
	right, _ := change.After().Get()
	if left[0] != 3 || right[0] != 4 {
		t.Fatal("change retained input storage")
	}
	left[0], right[0] = 7, 7
	left, _ = change.Before().Get()
	right, _ = change.After().Get()
	if left[0] != 3 || right[0] != 4 {
		t.Fatal("change getters exposed captured storage")
	}
	unchanged, err := lifecycle.CompareField(c, value.Set(binaryBlob{3}), value.Set(binaryBlob{3}), true)
	if err != nil || unchanged.Changed() || !unchanged.Assigned() {
		t.Fatal("equal bytes lost assignment semantics", err)
	}
	if (codec.Codec[binaryBlob]{}).CloneOptional(value.Optional[binaryBlob]{}).IsSet() {
		t.Fatal("omission became a value")
	}
}
