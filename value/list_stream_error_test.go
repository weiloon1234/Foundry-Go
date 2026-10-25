package value_test

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type listStreamCause struct {
	mode                 string
	formatted, inspected atomic.Int32
}

func (e *listStreamCause) Error() string {
	e.formatted.Add(1)
	if e.mode == "panic" {
		panic("private")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return "private list cause"
}
func (e *listStreamCause) Format(fmt.State, rune) {
	e.formatted.Add(1)
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	panic("private formatter")
}
func (e *listStreamCause) Is(error) bool {
	e.inspected.Add(1)
	if e.mode == "panic" {
		panic("private Is")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return false
}
func (e *listStreamCause) As(any) bool {
	e.inspected.Add(1)
	if e.mode == "panic" {
		panic("private As")
	}
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	return false
}
func (e *listStreamCause) Unwrap() error {
	if e.mode == "cycle" {
		return e
	}
	return nil
}

type listBytesMethod struct{ cause *listStreamCause }

func (v listBytesMethod) MarshalJSON() ([]byte, error) { return nil, v.cause }

type listStreamMethod struct{ cause *listStreamCause }

func (v listStreamMethod) MarshalJSONTo(*jsontext.Encoder) error { return v.cause }

type listTextMethod struct{ cause *listStreamCause }

func (v listTextMethod) MarshalText() ([]byte, error) { return nil, v.cause }

type listAppendMethod struct{ cause *listStreamCause }

func (v listAppendMethod) AppendText([]byte) ([]byte, error) { return nil, v.cause }

type absentListError struct{}

func (*absentListError) Error() string { return "unmatched" }

func TestNativeListStreamingContainsMethodErrors(t *testing.T) {
	for _, mode := range []string{"private", "panic", "goexit", "cycle"} {
		for name, input := range map[string]func(*listStreamCause) any{
			"MarshalJSON":    func(e *listStreamCause) any { return value.List[listBytesMethod]{{e}} },
			"Optional List":  func(e *listStreamCause) any { return value.Set(value.List[listBytesMethod]{{e}}) },
			"Nullable List":  func(e *listStreamCause) any { return value.Of(value.List[listBytesMethod]{{e}}) },
			"MarshalJSONTo":  func(e *listStreamCause) any { return value.List[listStreamMethod]{{e}} },
			"MarshalText":    func(e *listStreamCause) any { return value.List[listTextMethod]{{e}} },
			"AppendText":     func(e *listStreamCause) any { return value.List[listAppendMethod]{{e}} },
			"map text key":   func(e *listStreamCause) any { return value.List[map[listTextMethod]int]{{{e}: 1}} },
			"map append key": func(e *listStreamCause) any { return value.List[map[listAppendMethod]int]{{{e}: 1}} },
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				cause := &listStreamCause{mode: mode}
				_, err := jsonv2.Marshal(input(cause))
				if err == nil {
					t.Fatal("native List accepted callback failure")
				}
				var typed *listStreamCause
				if !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause {
					t.Fatal("native List lost original error identity")
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.formatted.Load() != 0 {
					t.Fatal("native List invoked private formatter")
				}
				var absent *absentListError
				if errors.Is(err, errors.New("missing")) || errors.As(err, &absent) {
					t.Fatal("unexpected error match")
				}
				if cause.inspected.Load() > 256 {
					t.Fatal("native List error inspection was unbounded")
				}
			})
		}
	}
}

func TestNativeListStreamingKeepsCallerMethodOverride(t *testing.T) {
	cause := &listStreamCause{mode: "panic"}
	custom := jsonv2.WithMarshalers(jsonv2.MarshalFunc[listBytesMethod](func(listBytesMethod) ([]byte, error) { return []byte(`"custom"`), nil }))
	data, err := jsonv2.Marshal(value.List[listBytesMethod]{{cause}}, custom)
	if err != nil || string(data) != `["custom"]` || cause.formatted.Load() != 0 || cause.inspected.Load() != 0 {
		t.Fatalf("caller override changed: %s %v", data, err)
	}
}

type listDecodeBytes int

func (*listDecodeBytes) UnmarshalJSON(data []byte) error { return listDecodeCause(data) }

type listDecodeStream int

func (*listDecodeStream) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	data, err := dec.ReadValue()
	if err != nil {
		return err
	}
	return listDecodeCause(data)
}

type listDecodeText int

func (*listDecodeText) UnmarshalText(data []byte) error { return &listStreamCause{mode: string(data)} }
func listDecodeCause(data []byte) error {
	var mode string
	if err := jsonv2.Unmarshal(data, &mode); err != nil {
		return err
	}
	return &listStreamCause{mode: mode}
}

func TestNativeJSONValueWrappersContainDecoderErrors(t *testing.T) {
	for _, mode := range []string{"private", "panic", "goexit", "cycle"} {
		data := []byte(`"` + mode + `"`)
		for name, decode := range map[string]func() error{
			"List UnmarshalJSON": func() error {
				var target value.List[listDecodeBytes]
				return jsonv2.Unmarshal(append(append([]byte{'['}, data...), ']'), &target)
			},
			"List UnmarshalJSONFrom": func() error {
				var target value.List[listDecodeStream]
				return jsonv2.Unmarshal(append(append([]byte{'['}, data...), ']'), &target)
			},
			"List UnmarshalText": func() error {
				var target value.List[listDecodeText]
				return jsonv2.Unmarshal(append(append([]byte{'['}, data...), ']'), &target)
			},
			"Optional": func() error { var target value.Optional[listDecodeBytes]; return jsonv2.Unmarshal(data, &target) },
			"Nullable": func() error { var target value.Nullable[listDecodeStream]; return jsonv2.Unmarshal(data, &target) },
			"JSON":     func() error { var target value.JSON[listDecodeBytes]; return jsonv2.Unmarshal(data, &target) },
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				err := decode()
				var cause *listStreamCause
				if err == nil || !errors.As(err, &cause) || cause.mode != mode || !errors.Is(err, cause) {
					t.Fatal("native wrapper lost decoder identity")
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || cause.formatted.Load() != 0 {
					t.Fatal("native decoder error formatted")
				}
				var absent *absentListError
				if errors.Is(err, errors.New("missing")) || errors.As(err, &absent) {
					t.Fatal("unexpected decoder error match")
				}
			})
		}
	}
}

type listExitMethod string

func (v listExitMethod) MarshalJSON() ([]byte, error) {
	if v == "goexit" {
		runtime.Goexit()
	}
	panic("private callback panic")
}
func TestNativeListContainsCallbackPanicAndGoexit(t *testing.T) {
	for _, mode := range []listExitMethod{"panic", "goexit"} {
		_, err := jsonv2.Marshal(value.List[listExitMethod]{mode})
		if !errors.Is(err, fault.Panicked) {
			t.Fatal("native List callback exit escaped containment")
		}
		if strings.Contains(fmt.Sprint(err), "private") {
			t.Fatal("native List callback panic leaked")
		}
	}
}
