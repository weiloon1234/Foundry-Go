package jsoncodec_test

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
)

type hostile struct {
	mode                 string
	formatted, inspected atomic.Int32
}

func (e *hostile) Error() string {
	e.formatted.Add(1)
	if e.mode == "panic" {
		panic("private")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return "private"
}
func (e *hostile) Format(fmt.State, rune) {
	e.formatted.Add(1)
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	panic("private formatter")
}
func (e *hostile) Is(error) bool {
	e.inspected.Add(1)
	if e.mode == "panic" {
		panic("private Is")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return false
}
func (e *hostile) As(any) bool {
	e.inspected.Add(1)
	if e.mode == "panic" {
		panic("private As")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return false
}
func (e *hostile) Unwrap() error {
	if e.mode == "cycle" {
		return e
	}
	return nil
}

type marshaler struct{ Cause error }

func (v marshaler) MarshalJSON() ([]byte, error) { return nil, v.Cause }

type pointerMarshaler struct{ Cause *hostile }

func (v *pointerMarshaler) MarshalJSON() ([]byte, error) { return nil, v.Cause }

type marshalerTo struct{ Cause error }

func (v marshalerTo) MarshalJSONTo(*jsontext.Encoder) error { return v.Cause }

type textMarshaler struct{ Cause *hostile }

func (v textMarshaler) MarshalText() ([]byte, error) { return nil, v.Cause }

type textAppender struct{ Cause *hostile }

func (v textAppender) AppendText([]byte) ([]byte, error) { return nil, v.Cause }

type unmarshaler struct{ Cause error }

func (v *unmarshaler) UnmarshalJSON([]byte) error { return v.Cause }

type unmarshalerFrom struct{ Cause error }

func (v *unmarshalerFrom) UnmarshalJSONFrom(*jsontext.Decoder) error { return v.Cause }

type textUnmarshaler struct{ Cause error }

func (v *textUnmarshaler) UnmarshalText([]byte) error { return v.Cause }

func TestApplicationErrorsStayOpaqueInsideJSON(t *testing.T) {
	for _, mode := range []string{"private", "panic", "goexit", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			cases := map[string]func(*hostile) error{
				"MarshalJSON":         func(e *hostile) error { _, err := jsoncodec.Marshal(marshaler{e}); return err },
				"MarshalJSONTo":       func(e *hostile) error { _, err := jsoncodec.Marshal(marshalerTo{e}); return err },
				"pointer MarshalJSON": func(e *hostile) error { _, err := jsoncodec.Marshal(&pointerMarshaler{e}); return err },
				"addressable field MarshalJSON": func(e *hostile) error {
					_, err := jsoncodec.Marshal(&struct{ Value pointerMarshaler }{pointerMarshaler{e}})
					return err
				},
				"MarshalText":       func(e *hostile) error { _, err := jsoncodec.Marshal(textMarshaler{e}); return err },
				"AppendText":        func(e *hostile) error { _, err := jsoncodec.Marshal(textAppender{e}); return err },
				"map MarshalText":   func(e *hostile) error { _, err := jsoncodec.Marshal(map[textMarshaler]int{{e}: 1}); return err },
				"map AppendText":    func(e *hostile) error { _, err := jsoncodec.Marshal(map[textAppender]int{{e}: 1}); return err },
				"UnmarshalJSON":     func(e *hostile) error { return jsoncodec.Unmarshal([]byte(`{}`), &unmarshaler{e}) },
				"UnmarshalJSONFrom": func(e *hostile) error { return jsoncodec.Unmarshal([]byte(`{}`), &unmarshalerFrom{e}) },
				"UnmarshalText":     func(e *hostile) error { return jsoncodec.Unmarshal([]byte(`"value"`), &textUnmarshaler{e}) },
			}
			for name, run := range cases {
				t.Run(name, func(t *testing.T) {
					cause := &hostile{mode: mode}
					err := run(cause)
					if err == nil {
						t.Fatal("callback failure was lost")
					}
					if strings.Contains(fmt.Sprint(err), "private") || cause.formatted.Load() != 0 {
						t.Fatal("application formatter executed")
					}
					if !errors.Is(err, cause) {
						t.Fatal("sentinel identity lost")
					}
					var typed *hostile
					if !errors.As(err, &typed) || typed != cause {
						t.Fatal("concrete cause identity lost")
					}
					if cause.formatted.Load() != 0 {
						t.Fatal("cause formatting executed")
					}
					if mode == "panic" || mode == "goexit" {
						if !errors.Is(err, fault.Panicked) {
							t.Fatal("native error-inspection failure classification changed")
						}
					}
					if cause.inspected.Load() == 0 || cause.inspected.Load() > 65 {
						t.Fatal("JSON error inspection was missing or unbounded")
					}
				})
			}
		})
	}
}

type exitMarshaler string

func (v exitMarshaler) MarshalJSON() ([]byte, error) {
	if v == "panic" {
		panic("private")
	}
	runtime.Goexit()
	return nil, nil
}

type exitUnmarshaler string

func (v *exitUnmarshaler) UnmarshalJSON([]byte) error {
	if *v == "panic" {
		panic("private")
	}
	runtime.Goexit()
	return nil
}
func TestJSONContainsPanicAndGoexit(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		_, err := jsoncodec.Marshal(exitMarshaler(mode))
		if !errors.Is(err, fault.Panicked) {
			t.Fatal("marshaler exit was lost")
		}
		input := exitUnmarshaler(mode)
		err = jsoncodec.Unmarshal([]byte(`null`), &input)
		if !errors.Is(err, fault.Panicked) {
			t.Fatal("unmarshaler exit was lost")
		}
	}
}

type pointerOnly int

func (*pointerOnly) MarshalJSON() ([]byte, error) { return []byte(`"pointer"`), nil }

func (v *pointerOnly) UnmarshalJSON(data []byte) error {
	if string(data) == `"pointer"` {
		*v = 1
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*v = pointerOnly(n)
	return nil
}

type uintptrFallback uintptr

func (uintptrFallback) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*uintptrFallback) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

type streamingFallback int

func (streamingFallback) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*streamingFallback) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

type prioritized int

func (prioritized) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue([]byte(`"stream"`))
}
func (prioritized) MarshalJSON() ([]byte, error) { return []byte(`"bytes"`), nil }

type textPriority int

func (textPriority) AppendText(dst []byte) ([]byte, error) { return append(dst, "append"...), nil }
func (textPriority) MarshalText() ([]byte, error)          { return []byte("text"), nil }

type stringKey string

func (stringKey) MarshalText() ([]byte, error) { return []byte("mapped"), nil }

func TestJSONPreservesNativeMethodSelection(t *testing.T) {
	pointer := pointerOnly(1)
	inputs := []any{pointer, &pointer, []pointerOnly{1}, map[string]pointerOnly{"key": 1}, struct{ Value pointerOnly }{1}, &struct{ Value pointerOnly }{1}, streamingFallback(42), prioritized(1), textPriority(1), map[stringKey]int{"key": 1}, map[textPriority]int{1: 1}, (*pointerOnly)(nil)}
	for _, input := range inputs {
		expected, expectedErr := jsonv2.Marshal(input, json.DefaultOptionsV1())
		// The shared owner retains encoding/json's persisted string-key contract,
		// even where native v2 custom-marshaler hooks select a text method first.
		if _, stringKeys := input.(map[stringKey]int); stringKeys {
			expected, expectedErr = []byte(`{"key":1}`), nil
		}
		actual, actualErr := jsoncodec.Marshal(input)
		if expectedErr != nil || actualErr != nil || string(actual) != string(expected) {
			t.Fatalf("%T: got %s (%v), want %s (%v)", input, actual, actualErr, expected, expectedErr)
		}
	}
	var decoded streamingFallback
	if err := jsoncodec.Unmarshal([]byte(`42`), &decoded); err != nil || decoded != 42 {
		t.Fatalf("streaming fallback changed: %d %v", decoded, err)
	}
	var actual map[string]any
	if err := jsoncodec.UnmarshalExact([]byte(`{"n":9007199254740993}`), &actual); err != nil || !reflect.DeepEqual(actual, map[string]any{"n": json.Number("9007199254740993")}) {
		t.Fatalf("exact numbers changed: %#v %v", actual, err)
	}
}

type fallbackEmbedded struct {
	Name string `json:"name"`
}

func (fallbackEmbedded) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*fallbackEmbedded) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

type promotedFallback struct {
	fallbackEmbedded
	Extra int `json:"extra"`
}
type directFallback struct {
	Pointer pointerOnly `json:"pointer"`
	Name    string      `json:"name,omitempty"`
	Hidden  int         `json:"-"`
	private int
}

func (directFallback) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*directFallback) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

type fallbackSlice []int

func (fallbackSlice) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*fallbackSlice) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

type fallbackMap map[string]int

func (fallbackMap) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*fallbackMap) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

func TestStreamingFallbackPreservesNativeRepresentations(t *testing.T) {
	inputs := []any{uintptrFallback(42), promotedFallback{fallbackEmbedded{"embedded"}, 2}, directFallback{1, "value", 4, 3}, &directFallback{1, "pointer", 4, 3}, fallbackSlice{1, 2}, fallbackMap{"value": 3}}
	for _, input := range inputs {
		expected, expectedErr := jsonv2.Marshal(input, json.DefaultOptionsV1())
		actual, actualErr := jsoncodec.Marshal(input)
		if expectedErr != nil || actualErr != nil || string(actual) != string(expected) {
			t.Fatalf("%T fallback got %s (%v), want %s (%v)", input, actual, actualErr, expected, expectedErr)
		}
		actualTarget := reflect.New(reflect.TypeOf(input))
		expectedTarget := reflect.New(reflect.TypeOf(input))
		expectedErr = jsonv2.Unmarshal(expected, expectedTarget.Interface(), json.DefaultOptionsV1())
		actualErr = jsoncodec.Unmarshal(expected, actualTarget.Interface())
		if expectedErr != nil || actualErr != nil || !reflect.DeepEqual(actualTarget.Interface(), expectedTarget.Interface()) {
			t.Fatalf("%T fallback decoding changed: %v %v", input, actualErr, expectedErr)
		}
	}
}

type modernKey string

func (modernKey) MarshalJSON() ([]byte, error) { return []byte(`"modern"`), nil }
func (*modernKey) UnmarshalJSON([]byte) error  { return nil }
func TestJSONMethodOptionsRemainEffective(t *testing.T) {
	input := map[modernKey]int{"original": 1}
	expected, err := jsonv2.Marshal(input, json.DefaultOptionsV1(), json.CallMethodsWithLegacySemantics(false))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := jsoncodec.Marshal(input, json.CallMethodsWithLegacySemantics(false))
	if err != nil || string(actual) != string(expected) {
		t.Fatalf("modern key methods changed: %s %v, want %s", actual, err, expected)
	}
}

type nestedStreaming struct{ Mode string }

func (v *nestedStreaming) MarshalJSONTo(enc *jsontext.Encoder) error {
	local := pointerOnly(7)
	var input any
	switch v.Mode {
	case "value":
		input = local
	case "pointer":
		input = &local
	case "slice":
		input = []pointerOnly{local}
	case "map":
		input = map[string]pointerOnly{"local": local}
	case "struct":
		input = struct{ Value pointerOnly }{local}
	}
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("nested")); err != nil {
		return err
	}
	if err := jsonv2.MarshalEncode(enc, input); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}
func TestStreamingMethodNativeNestedCallsKeepTheirOwnAddressability(t *testing.T) {
	for _, mode := range []string{"value", "pointer", "slice", "map", "struct"} {
		input := &struct{ Field nestedStreaming }{nestedStreaming{mode}}
		expected, expectedErr := jsonv2.Marshal(input, json.DefaultOptionsV1())
		actual, actualErr := jsoncodec.Marshal(input)
		if expectedErr != nil || actualErr != nil || string(actual) != string(expected) {
			t.Fatalf("%s: got %s (%v), want %s (%v)", mode, actual, actualErr, expected, expectedErr)
		}
	}
}

type collidingFallback struct {
	fallbackEmbedded
	Embedded0 string `json:"separate"`
}

type recursiveFallback struct {
	*recursiveFallback
	Name string `json:"name"`
}

func (recursiveFallback) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*recursiveFallback) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }

func TestStreamingFallbackPreservesEmbeddedNameCollisionsAndRecursiveTypes(t *testing.T) {
	inputs := []any{collidingFallback{fallbackEmbedded{"nested"}, "sibling"}, recursiveFallback{Name: "root"}}
	for _, input := range inputs {
		expected, expectedErr := jsonv2.Marshal(input, json.DefaultOptionsV1())
		actual, actualErr := jsoncodec.Marshal(input)
		if expectedErr != nil || actualErr != nil || string(actual) != string(expected) {
			t.Fatalf("%T: got %s (%v), want %s (%v)", input, actual, actualErr, expected, expectedErr)
		}
		expectedTarget, actualTarget := reflect.New(reflect.TypeOf(input)), reflect.New(reflect.TypeOf(input))
		expectedErr = jsonv2.Unmarshal(expected, expectedTarget.Interface(), json.DefaultOptionsV1())
		actualErr = jsoncodec.Unmarshal(expected, actualTarget.Interface())
		if expectedErr != nil || actualErr != nil || !reflect.DeepEqual(actualTarget.Interface(), expectedTarget.Interface()) {
			t.Fatalf("%T: fallback decode changed: %v %v", input, actualErr, expectedErr)
		}
	}
}

func TestPointerMapKeyNilAndMethodParity(t *testing.T) {
	integer := 1
	text := textPriority(1)
	for _, input := range []any{map[*int]int{nil: 1}, map[*int]int{&integer: 1}, map[*textPriority]int{nil: 1}, map[*textPriority]int{&text: 1}} {
		expected, expectedErr := jsonv2.Marshal(input, json.DefaultOptionsV1())
		actual, actualErr := jsoncodec.Marshal(input)
		if (expectedErr == nil) != (actualErr == nil) || string(expected) != string(actual) {
			t.Fatalf("%T: got %s (%v), want %s (%v)", input, actual, actualErr, expected, expectedErr)
		}
	}
}
