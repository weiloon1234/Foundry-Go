package contract

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type privateDecodeError struct{ formatted atomic.Int32 }

func (e *privateDecodeError) Error() string {
	e.formatted.Add(1)
	runtime.Goexit()
	return "private codec"
}
func (e *privateDecodeError) Format(fmt.State, rune) { e.formatted.Add(1); panic("private codec") }

var containedDecodeCause = &privateDecodeError{}

type ContainedJSONField struct{}

func (*ContainedJSONField) UnmarshalJSON([]byte) error { return containedDecodeCause }

type ContainedJSONDTO struct {
	Field ContainedJSONField `json:"field"`
}

func TestJSONDTOAndUnionContainErrorMethods(t *testing.T) {
	root := dtoRoot[ContainedJSONDTO]()
	descriptor := DefineJSON[ContainedJSONDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{{Name: "field", Type: "field", Required: true}}},
		{ID: "field", Kind: ObjectKind},
	}})
	_, dtoErr := descriptor.Decode(t.Context(), []byte(`{"field":{}}`), dtoLimits())
	stored := UnionValue[ContainedJSONDTO]{wire: `{"kind":"payload","field":{}}`, payload: `{"field":{}}`, tag: "payload"}
	_, unionErr := UnionVariant[ContainedJSONDTO, ContainedJSONDTO](stored, "payload")
	for _, err := range []error{dtoErr, unionErr} {
		var typed *privateDecodeError
		if !errors.Is(err, containedDecodeCause) || !errors.As(err, &typed) || typed != containedDecodeCause {
			t.Fatal("transport decoder lost cause identity")
		}
		if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
			t.Fatal("private text leaked")
		}
	}
	if containedDecodeCause.formatted.Load() != 0 {
		t.Fatal("private error formatting was called")
	}
}
