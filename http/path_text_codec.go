package http

import (
	"encoding"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// PathTextPointer is the pointer method set required by TextPath. It accepts
// both pointer and value marshal methods while retaining the concrete value V.
type PathTextPointer[V any] interface {
	*V
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

// TextPath uses a type's existing text codec, including its validation. Generated
// enum membership, exact decimal and temporal representations keep their source
// of truth. Custom methods must be deterministic, respect input ownership and be
// safe for concurrent calls. Path catches panic/Goexit, waits for codec completion
// and validates the resulting text. Direct codec calls do not add that boundary.
// Known decimal/temporal types carry automatic metadata. Use EnumPath for enum
// case metadata, or DescribeURL to describe a custom type explicitly.
func TextPath[V any, P PathTextPointer[V]]() PathCodec[V] { return knownTextURLCodec[V, P]() }

type textPathCodec[V any, P PathTextPointer[V]] struct{}

func (textPathCodec[V, P]) Parse(text string) (V, error) {
	var decoded V
	if err := P(&decoded).UnmarshalText([]byte(text)); err != nil {
		var zero V
		return zero, fault.Wrap(fault.Invalid, "invalid path text representation", err)
	}
	return decoded, nil
}

func (textPathCodec[V, P]) Format(value V) (string, error) {
	data, err := P(&value).MarshalText()
	if err != nil {
		return "", fault.Wrap(fault.Invalid, "path text value could not be encoded", err)
	}
	return string(data), nil
}

// BoolPath preserves a named or ordinary boolean using exactly true and false.
func BoolPath[V ~bool]() PathCodec[V] {
	return nativeURLCodec[V](boolPathCodec[V]{}, contract.Type{Kind: contract.BooleanKind}, BooleanURLSyntax)
}

type boolPathCodec[V ~bool] struct{}

func (boolPathCodec[V]) Parse(text string) (V, error) {
	switch text {
	case "true":
		return V(true), nil
	case "false":
		return V(false), nil
	default:
		return V(false), fault.New(fault.Invalid, "invalid boolean path parameter")
	}
}

func (boolPathCodec[V]) Format(value V) (string, error) {
	if value {
		return "true", nil
	}
	return "false", nil
}
