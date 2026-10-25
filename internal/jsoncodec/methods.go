package jsoncodec

import (
	"encoding"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type callbackState struct {
	panicked   error
	diagnostic string
}

// Each returned capsule owns its metadata, including failures collected before
// a later sibling. Native JSON can traverse only the sealed error. Framework
// discovery uses one bounded, contained walk of the actual returned graph.
type callbackFailure struct {
	opaque   error
	metadata callbackState
}

func (*callbackFailure) Error() string   { return "JSON callback failed" }
func (e *callbackFailure) Unwrap() error { return e.opaque }

func callbackMetadata(err error) callbackState {
	var state callbackState
	complete := callback.ErrorWalk(err, func(current error) bool {
		if capsule, ok := current.(*callbackFailure); ok {
			if state.panicked == nil {
				state.panicked = capsule.metadata.panicked
			}
			if state.diagnostic == "" {
				state.diagnostic = capsule.metadata.diagnostic
			}
		}
		return true
	})
	if !complete {
		// An incomplete graph cannot establish malformed input. Preserve the
		// original graph below, with a trusted internal-failure classification.
		state.panicked = safeerror.NewFault(fault.Panicked, "JSON callback metadata inspection failed")
		state.diagnostic = ""
	}
	return state
}

func (s *callbackState) capturePanic(err error) {
	if safeerror.IsPanic(err) {
		s.panicked = err
	} else if failure, ok := err.(*fault.Error); ok && failure != nil && failure.Code() == fault.Panicked {
		s.panicked = safeerror.WrapFailure(fault.Panicked, "JSON callback failed", err)
	}
}
func (s *callbackState) finish(message string, err error) error {
	if err == nil {
		return nil
	}
	state := callbackMetadata(err)
	result := safeerror.Reveal(message, err)
	if state.panicked != nil {
		return safeerror.Retain(state.panicked, safeerror.Reveal(message, safeerror.Join(message, state.panicked, err)))
	}
	if state.diagnostic != "" {
		return safeerror.Owned(state.diagnostic, result)
	}
	return callback.RetainDiagnostic(err, result)
}

type marshalFrame struct {
	kind        reflect.Kind
	addressable bool
	index       int64
	pending     bool
}
type marshalState struct {
	callbackState
	frames          map[int]marshalFrame
	rootDepth       int
	rootAddressable bool
}

// The hook receives addressable copies too. Track the native container's
// addressability so legacy pointer methods still skip map values and value roots.
func (s *marshalState) receiver(enc *jsontext.Encoder, input any) (reflect.Value, any) {
	value := reflect.ValueOf(input).Elem()
	depth := enc.StackDepth()
	_, index := enc.StackIndex(depth)
	if s.frames == nil {
		s.frames = make(map[int]marshalFrame)
	}
	addressable := depth == s.rootDepth && s.rootAddressable
	if parent, ok := s.frames[depth-1]; ok && depth > s.rootDepth {
		addressable = parent.kind == reflect.Slice || parent.kind != reflect.Map && parent.addressable
	}
	current := s.frames[depth]
	if current.pending && current.index == index {
		addressable = current.addressable
	}
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		s.frames[depth] = marshalFrame{index: index, pending: true, addressable: value.Kind() == reflect.Pointer}
		return value, nil
	}
	s.frames[depth] = marshalFrame{kind: value.Kind(), addressable: addressable, index: index}
	legacy, _ := jsonv2.GetOption(enc.Options(), json.CallMethodsWithLegacySemantics)
	if !legacy || addressable {
		return value, input
	}
	return value, value.Interface()
}

func (s *marshalState) marshal(enc *jsontext.Encoder, input any) error {
	kind, n := enc.StackIndex(enc.StackDepth())
	legacy, _ := jsonv2.GetOption(enc.Options(), json.CallMethodsWithLegacySemantics)
	key := legacy && kind == '{' && n%2 == 0
	original := reflect.ValueOf(input).Elem()
	if key && original.Kind() == reflect.Pointer {
		_, textMethod := original.Interface().(encoding.TextMarshaler)
		_, appendMethod := original.Interface().(encoding.TextAppender)
		if textMethod || appendMethod {
			if original.IsNil() {
				return enc.WriteToken(jsontext.String(""))
			}
			return s.marshalRemaining(enc, original.Interface(), original, false)
		}
	}
	value, receiver := s.receiver(enc, input)
	if receiver == nil {
		return errors.ErrUnsupported
	}
	if !key {
		if method, ok := receiver.(jsonv2.MarshalerTo); ok {
			depth := enc.StackDepth()
			_, length := enc.StackIndex(depth)
			// A streaming callback owns native JSON calls made in its body.
			// Start their addressability frames independently of our caller;
			// native hooks do not expose their root's forced-address metadata.
			err, skipped := s.invoke("MarshalJSONTo", func() error {
				originalFrames, originalDepth, originalAddressable := s.frames, s.rootDepth, s.rootAddressable
				s.frames, s.rootDepth, s.rootAddressable = nil, enc.StackDepth(), false
				defer func() { s.frames, s.rootDepth, s.rootAddressable = originalFrames, originalDepth, originalAddressable }()
				return method.MarshalJSONTo(enc)
			}, true)
			if !skipped {
				return err
			}
			_, after := enc.StackIndex(enc.StackDepth())
			if depth != enc.StackDepth() || length != after {
				return safeerror.New("JSON codec changed output before declining")
			}
			return s.marshalRemaining(enc, receiver, value, true)
		}
	}
	return s.marshalRemaining(enc, receiver, value, false)
}

func (s *marshalState) marshalRemaining(enc *jsontext.Encoder, receiver any, value reflect.Value, skipped bool) error {
	kind, n := enc.StackIndex(enc.StackDepth())
	legacy, _ := jsonv2.GetOption(enc.Options(), json.CallMethodsWithLegacySemantics)
	key := legacy && kind == '{' && n%2 == 0
	if !key {
		if method, ok := receiver.(json.Marshaler); ok {
			var data []byte
			err, _ := s.invoke("MarshalJSON", func() (err error) { data, err = method.MarshalJSON(); return err }, false)
			if err != nil {
				return err
			}
			return enc.WriteValue(data)
		}
	}
	var data []byte
	var err error
	switch method := receiver.(type) {
	case encoding.TextAppender:
		err, _ = s.invoke("AppendText", func() (err error) { data, err = method.AppendText(nil); return err }, false)
	case encoding.TextMarshaler:
		err, _ = s.invoke("MarshalText", func() (err error) { data, err = method.MarshalText(); return err }, false)
	default:
		if !skipped {
			return errors.ErrUnsupported
		}
		bare, err := withoutMethods(value)
		if err != nil {
			return err
		}
		plain := bare.value.Interface()
		if reflect.TypeOf(receiver).Kind() == reflect.Pointer {
			plain = bare.value.Addr().Interface()
		}
		depth := enc.StackDepth()
		_, index := enc.StackIndex(depth)
		s.frames[depth] = marshalFrame{pending: true, index: index, addressable: reflect.TypeOf(plain).Kind() == reflect.Pointer}
		return jsonv2.MarshalEncode(enc, plain)
	}
	if err != nil {
		return err
	}
	return enc.WriteToken(jsontext.String(string(data)))
}

// invoke keeps the original error opaque even while JSON checks ErrUnsupported.
func (s *callbackState) invoke(operation string, fn func() error, maySkip bool) (result error, declined bool) {
	var state callbackState
	defer func() {
		if result != nil {
			result = &callbackFailure{opaque: result, metadata: state}
		}
	}()
	var cause error
	contained := callback.Isolated(operation, func() error { cause = fn(); return nil })
	if contained != nil {
		state.capturePanic(contained)
		return safeerror.Seal(operation+" failed", contained), false
	}
	if cause == nil {
		return nil, false
	}
	if stream, ok := cause.(*streamFailure); ok {
		if message, owned := safeerror.OwnedMessage(cause); owned {
			state.diagnostic = message
		}
		state.capturePanic(stream.panicked)
		return safeerror.Seal(operation+" failed", cause), false
	}
	// Native JSON inspects every method error for ErrUnsupported, including
	// non-streaming methods that cannot decline their representation.
	var skipped bool
	inspected := callback.Isolated("inspect JSON fallback", func() error { skipped = errorgraph.Is(cause, errors.ErrUnsupported); return nil })
	if inspected != nil {
		failure := safeerror.WrapFailure(fault.Panicked, "JSON error inspection failed", cause)
		state.capturePanic(failure)
		return safeerror.Seal(operation+" failed", failure), false
	}
	if maySkip && skipped {
		return nil, true
	}
	state = callbackMetadata(cause)
	state.capturePanic(cause)
	if message, owned := safeerror.OwnedMessage(cause); owned {
		state.diagnostic = message
	}
	return safeerror.Seal(operation+" failed", cause), false
}

type unmarshalState struct{ callbackState }

func (s *unmarshalState) unmarshal(dec *jsontext.Decoder, input any) error {
	value := reflect.ValueOf(input).Elem()
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		return errors.ErrUnsupported
	}
	kind, n := dec.StackIndex(dec.StackDepth())
	legacy, _ := jsonv2.GetOption(dec.Options(), json.CallMethodsWithLegacySemantics)
	key := legacy && kind == '{' && n%2 == 0
	if !key {
		if method, ok := input.(jsonv2.UnmarshalerFrom); ok {
			depth := dec.StackDepth()
			_, length := dec.StackIndex(depth)
			err, skipped := s.invoke("UnmarshalJSONFrom", func() error { return method.UnmarshalJSONFrom(dec) }, true)
			if !skipped {
				return err
			}
			_, after := dec.StackIndex(dec.StackDepth())
			if depth != dec.StackDepth() || length != after {
				return safeerror.New("JSON codec changed input before declining")
			}
			if _, ok := input.(json.Unmarshaler); !ok {
				if _, ok := input.(encoding.TextUnmarshaler); !ok {
					bare, err := withoutMethods(value)
					if err != nil {
						return err
					}
					if err := jsonv2.UnmarshalDecode(dec, bare.value.Addr().Interface()); err != nil {
						return err
					}
					return bare.restore()
				}
			}
		}
		if method, ok := input.(json.Unmarshaler); ok {
			data, err := dec.ReadValue()
			if err != nil {
				return err
			}
			err, _ = s.invoke("UnmarshalJSON", func() error { return method.UnmarshalJSON(data) }, false)
			return err
		}
	}
	if method, ok := input.(encoding.TextUnmarshaler); ok {
		data, err := dec.ReadValue()
		if err != nil {
			return err
		}
		if string(data) == "null" {
			legacy, _ := jsonv2.GetOption(dec.Options(), json.MergeWithLegacySemantics)
			if !legacy {
				value.SetZero()
			}
			return nil
		}
		var text string
		err = json.Unmarshal(data, &text)
		if err != nil {
			return safeerror.New("JSON text codec requires a string")
		}
		err, _ = s.invoke("UnmarshalText", func() error { return method.UnmarshalText([]byte(text)) }, false)
		return err
	}
	return errors.ErrUnsupported
}
