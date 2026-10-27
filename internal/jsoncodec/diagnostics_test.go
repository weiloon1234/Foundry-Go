package jsoncodec_test

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type diagnosticCause struct {
	mode      string
	formatted atomic.Int32
}

func (e *diagnosticCause) Error() string {
	e.formatted.Add(1)
	if e.mode == "goexit" {
		runtime.Goexit()
	}
	panic("private diagnostic")
}
func (e *diagnosticCause) Format(fmt.State, rune) { _ = e.Error() }

type diagnosticJSON struct{ failure error }

func (v diagnosticJSON) MarshalJSON() ([]byte, error) { return nil, v.failure }
func (v *diagnosticJSON) UnmarshalJSON([]byte) error  { return v.failure }

func TestJSONPipelineDiagnostics(t *testing.T) {
	for _, operation := range []struct {
		name   string
		run    func(error) error
		prefix string
	}{
		{"marshal", func(e error) error { _, err := jsoncodec.Marshal(diagnosticJSON{e}); return err }, "marshal JSON"},
		{"marshal stream", func(e error) error {
			var b bytes.Buffer
			return jsoncodec.MarshalEncode(jsontext.NewEncoder(&b), diagnosticJSON{e})
		}, "encode JSON stream"},
		{"marshal writer", func(e error) error { var b bytes.Buffer; return jsoncodec.MarshalWrite(&b, diagnosticJSON{e}) }, "marshal JSON"},
		{"unmarshal", func(e error) error { return jsoncodec.Unmarshal([]byte(`{}`), &diagnosticJSON{e}) }, "unmarshal JSON"},
		{"unmarshal exact", func(e error) error { return jsoncodec.UnmarshalExact([]byte(`{}`), &diagnosticJSON{e}) }, "unmarshal JSON"},
	} {
		for _, owned := range []bool{false, true} {
			for _, mode := range []string{"panic", "goexit"} {
				t.Run(fmt.Sprintf("%s/owned=%t/%s", operation.name, owned, mode), func(t *testing.T) {
					cause := &diagnosticCause{mode: mode}
					var input error = fault.Wrap(fault.Invalid, "private JSON diagnostic", cause)
					want := operation.prefix + " failed"
					if operation.name == "marshal stream" {
						want = "JSON stream encoding failed"
					}
					if owned {
						input = safeerror.WrapFault(fault.Invalid, "owned JSON diagnostic", cause)
						want = operation.prefix + ": invalid: owned JSON diagnostic"
					}
					err := operation.run(input)
					var retained *diagnosticCause
					if err == nil || err.Error() != want || !errors.Is(err, input) || !errors.Is(err, fault.Invalid) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("JSON boundary lost diagnostic, identity or classification", err)
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
					if cause.formatted.Load() != 0 {
						t.Fatal("JSON invoked application formatting")
					}
				})
			}
		}
	}
}

type decliningDiagnostic struct{ Value string }

func (decliningDiagnostic) MarshalJSONTo(*jsontext.Encoder) error {
	return safeerror.Owned("declined diagnostic", errors.ErrUnsupported)
}
func TestDeclinedDiagnosticDoesNotDescribeLaterFailure(t *testing.T) {
	cause := &diagnosticCause{}
	input := struct {
		First decliningDiagnostic
		Last  diagnosticJSON
	}{decliningDiagnostic{"ok"}, diagnosticJSON{cause}}
	_, err := jsoncodec.Marshal(input)
	if err == nil || strings.Contains(err.Error(), "declined diagnostic") || !errors.Is(err, cause) || cause.formatted.Load() != 0 {
		t.Fatal("declined callback was attributed to another failure")
	}
}

type handledNestedJSON struct {
	failure error
	mode    string
}

func (v handledNestedJSON) MarshalJSONTo(enc *jsontext.Encoder) error {
	err := jsonv2.MarshalEncode(enc, nestedJSONFailure(v))
	if err == nil {
		return errors.New("nested failure did not execute")
	}
	return enc.WriteToken(jsontext.String("fallback"))
}

type nestedJSONFailure handledNestedJSON

func (v nestedJSONFailure) MarshalJSON() ([]byte, error) {
	if v.mode == "panic" {
		panic("private nested callback")
	}
	if v.mode == "goexit" {
		runtime.Goexit()
	}
	return nil, v.failure
}
func TestHandledNestedJSONFailureDoesNotEscapeInvocation(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		cause := &diagnosticCause{}
		handled := handledNestedJSON{safeerror.Owned("handled nested diagnostic", cause), mode}
		data, err := jsoncodec.Marshal(handled)
		if err != nil || string(data) != `"fallback"` {
			t.Fatal("handled failure contaminated successful fallback", err)
		}
		_, err = jsoncodec.Marshal(struct {
			First handledNestedJSON
			Last  float64
		}{handled, math.NaN()})
		if err == nil || strings.Contains(err.Error(), "handled nested diagnostic") || errors.Is(err, fault.Panicked) || errors.Is(err, cause) || cause.formatted.Load() != 0 {
			t.Fatal("handled nested failure contaminated later native error", err)
		}
	}
}

func (v *handledNestedJSON) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	child := nestedJSONFailure(*v)
	if err := jsonv2.UnmarshalDecode(dec, &child); err == nil {
		return errors.New("nested decoder did not fail")
	}
	return nil
}
func (v *nestedJSONFailure) UnmarshalJSON([]byte) error {
	if v.mode == "panic" {
		panic("private nested decoder")
	}
	if v.mode == "goexit" {
		runtime.Goexit()
	}
	return v.failure
}
func TestHandledNestedJSONDecodeFailureDoesNotEscapeInvocation(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		cause := &diagnosticCause{}
		handled := handledNestedJSON{safeerror.Owned("handled nested decoder diagnostic", cause), mode}
		if err := jsoncodec.Unmarshal([]byte(`"input"`), &handled); err != nil {
			t.Fatal("handled decoder failure contaminated success", err)
		}
		output := struct {
			First handledNestedJSON
			Last  int
		}{First: handled}
		err := jsoncodec.Unmarshal([]byte(`{"First":"input","Last":"wrong"}`), &output)
		if err == nil || strings.Contains(err.Error(), "handled nested decoder diagnostic") || errors.Is(err, fault.Panicked) || errors.Is(err, cause) || cause.formatted.Load() != 0 {
			t.Fatal("handled decoder failure contaminated later error", err)
		}
	}
}

type unrelatedCallbackFailure struct{ formatted atomic.Int32 }

func (e *unrelatedCallbackFailure) Error() string {
	e.formatted.Add(1)
	panic("private unrelated callback")
}
func (*unrelatedCallbackFailure) Is(target error) bool { return target != errors.ErrUnsupported }

type handlesThenFails struct{ failure *unrelatedCallbackFailure }

func (v handlesThenFails) MarshalJSONTo(enc *jsontext.Encoder) error {
	_ = jsonv2.MarshalEncode(enc, nestedJSONFailure{mode: "panic"})
	return v.failure
}
func TestJSONCallbackStateRequiresExactReturnedCause(t *testing.T) {
	cause := &unrelatedCallbackFailure{}
	_, err := jsoncodec.Marshal(handlesThenFails{cause})
	if err == nil || safeerror.IsPanic(err) || !errors.Is(err, cause) || cause.formatted.Load() != 0 {
		t.Fatal("custom Is attributed a discarded callback panic to an unrelated error")
	}
}

type multipleNestedJSON struct {
	mode, result string
	cause        error
}

func (v multipleNestedJSON) MarshalJSONTo(enc *jsontext.Encoder) error {
	first := jsonv2.MarshalEncode(enc, nestedJSONFailure{mode: v.mode})
	second := jsonv2.MarshalEncode(enc, nestedJSONFailure{failure: v.cause})
	switch v.result {
	case "first":
		return first
	case "join":
		return errors.Join(first, second)
	case "second":
		return second
	default:
		return enc.WriteToken(jsontext.String("fallback"))
	}
}
func TestMultipleNestedJSONFailuresRetainReturnedMetadata(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		for _, result := range []string{"first", "join", "second", "handled"} {
			t.Run(mode+"/"+result, func(t *testing.T) {
				cause := &diagnosticCause{}
				data, err := jsoncodec.Marshal(multipleNestedJSON{mode, result, cause})
				if result == "handled" {
					if err != nil || string(data) != `"fallback"` {
						t.Fatal("handled children contaminated success")
					}
				} else {
					if err == nil || safeerror.IsPanic(err) != (result != "second") {
						t.Fatal("returned child metadata lost or borrowed panic")
					}
					if errors.Is(err, cause) != (result != "first") {
						t.Fatal("returned child graph lost or borrowed cause")
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				}
				if cause.formatted.Load() != 0 {
					t.Fatal("formatted child cause")
				}
			})
		}
	}
}
