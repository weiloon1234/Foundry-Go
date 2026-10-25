package jsoncodec

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// MarshalEncode protects framework streaming delegation while retaining the
// encoder's options. Caller-installed WithMarshalers functions keep precedence:
// their implementation and native invocation belong to that caller; this boundary
// contains the JSON/text methods selected after those functions decline.
func MarshalEncode(enc *jsontext.Encoder, input any) error {
	state := marshalState{rootDepth: enc.StackDepth(), rootAddressable: reflect.ValueOf(input).IsValid() && reflect.TypeOf(input).Kind() == reflect.Pointer}
	methods := jsonv2.MarshalToFunc[any](state.marshal)
	if inherited, ok := jsonv2.GetOption(enc.Options(), jsonv2.WithMarshalers); ok {
		methods = jsonv2.JoinMarshalers(inherited, methods)
	}
	err := callback.Isolated("encode JSON stream", func() error {
		return state.finish("JSON stream encoding failed", jsonv2.MarshalEncode(enc, input, jsonv2.WithMarshalers(methods)))
	})
	return MethodError(err)
}

// MethodError returns a safe native JSON callback failure. Native JSON owns the
// outer operation, so this retains contained Is/As without an outer Reveal call.
func MethodError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*streamFailure); ok {
		return err
	}
	state := callbackState{}
	state.capturePanic(err)
	return safeerror.Retain(err, &streamFailure{cause: err, panicked: state.panicked})
}

// A native caller owns the outer return, so no framework Reveal follows it.
// Keep native's fallback probe opaque and provide bounded, contained Is/As for
// callers without exposing the original cause to native error inspection.
type streamFailure struct {
	cause    error
	panicked error
}

func (*streamFailure) Error() string    { return "JSON stream encoding failed" }
func (*streamFailure) GoString() string { return "JSON stream encoding failed" }
func (e *streamFailure) Is(target error) bool {
	if target == errors.ErrUnsupported {
		return false
	}
	matched := false
	err := callback.Isolated("inspect JSON stream error", func() error { matched = errorgraph.Is(e.cause, target); return nil })
	return err == nil && matched
}
func (e *streamFailure) As(target any) bool {
	matched := false
	err := callback.Isolated("inspect JSON stream error", func() error { matched = errorgraph.AsTarget(e.cause, target); return nil })
	return err == nil && matched
}

// Shared graph inspection follows children with its own budget and pruning;
// forwarding native Is/As here would bypass a diagnostic-only child boundary.
// Native traversal can reach only this opaque capsule. A later framework
// Reveal can still open it, including when Optional or Nullable nests a List.
func (e *streamFailure) Unwrap() error      { return safeerror.Seal("JSON stream encoding failed", e.cause) }
func (*streamFailure) IsShallow(error) bool { return false }
func (*streamFailure) AsShallow(any) bool   { return false }
func (e *streamFailure) UnwrapBounded(budget int) ([]error, bool) {
	if budget < 1 {
		return nil, false
	}
	return []error{e.cause}, true
}
