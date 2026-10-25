package config

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"strings"
	"testing"
)

type errorJSON struct{ Cause error }

func (v *errorJSON) UnmarshalJSON(data []byte) error {
	v.Cause = &errorgraphtest.Error{Mode: strings.Trim(string(data), `"`)}
	return v.Cause
}

type privateJSONValue struct{ calls *int }

func (v privateJSONValue) MarshalJSON() ([]byte, error) {
	*v.calls++
	panic("must not marshal normalized input")
}
func TestConfigJSONContainsMethodsAndRejectsNonNormalizedObjects(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		key := JSON("value", func(s *struct{ Value errorJSON }) *errorJSON { return &s.Value })
		schema, err := New(key)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = schema.Load(struct{ Value errorJSON }{}, Inputs[struct{ Value errorJSON }]{Files: []Values{{Name: "test", Data: map[string]string{"value": `"` + mode + `"`}}}})
		var cause *errorgraphtest.Error
		if err == nil || !errors.As(err, &cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
			t.Fatal("configuration lost contained JSON cause")
		}
	}
	calls := 0
	schema, err := New(JSON("value", func(s *struct{ Value any }) *any { return &s.Value }))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{privateJSONValue{&calls}, []any{privateJSONValue{&calls}}, map[string]any{"x": privateJSONValue{&calls}}} {
		_, err := DecodeObject(map[string]any{"value": input}, schema, "test")
		if !errors.Is(err, fault.Invalid) || calls != 0 {
			t.Fatal("unnormalized configuration invoked a marshaler")
		}
	}
}
