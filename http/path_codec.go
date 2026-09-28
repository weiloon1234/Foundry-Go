package http

import (
	"reflect"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// PathCodec converts one decoded URL parameter to and from a concrete value.
// Implementations must be safe for concurrent use. Parse and Format must agree;
// neither receives escaped URL text. Path applies escaping exactly once.
type PathCodec[V any] interface {
	Parse(string) (V, error)
	Format(V) (string, error)
}

// StringPath preserves ordinary and named string types. Path rejects empty single
// segments, controls, dot segments and a slash-only segment. A final catch-all can
// contain slashes and can be empty.
func StringPath[V ~string]() PathCodec[V] {
	return nativeURLCodec[V](stringPathCodec[V]{}, contract.Type{Kind: contract.StringKind}, TextURLSyntax)
}

type stringPathCodec[V ~string] struct{}

func (stringPathCodec[V]) Parse(text string) (V, error)   { return V(text), nil }
func (stringPathCodec[V]) Format(value V) (string, error) { return string(value), nil }

// PathInteger is the set of integer types supported by IntegerPath. Named
// integer types retain their identity through parsing, fields and URL generation.
type PathInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// IntegerPath accepts decimal integers without plus signs, whitespace or leading
// zeroes. It checks the destination width before conversion, including named types.
func IntegerPath[V PathInteger]() PathCodec[V] {
	var zero V
	return nativeURLCodec[V](integerPathCodec[V]{}, contract.Type{Kind: contract.IntegerKind, Bits: uint8(reflect.TypeFor[V]().Bits()), Signed: !(^zero > zero)}, IntegerURLSyntax)
}

type integerPathCodec[V PathInteger] struct{}

func (integerPathCodec[V]) Parse(text string) (V, error) {
	var zero V
	// Conversion back to text detects overflow for narrower integer types. Using
	// the complemented zero distinguishes unsigned types without reflection.
	if ^zero > zero {
		parsed, err := strconv.ParseUint(text, 10, 64)
		value := V(parsed)
		if err != nil || strconv.FormatUint(uint64(value), 10) != text {
			return zero, fault.New(fault.Invalid, "invalid integer path parameter")
		}
		return value, nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	value := V(parsed)
	if err != nil || strconv.FormatInt(int64(value), 10) != text {
		return zero, fault.New(fault.Invalid, "invalid integer path parameter")
	}
	return value, nil
}

func (integerPathCodec[V]) Format(value V) (string, error) {
	var zero V
	if ^zero > zero {
		return strconv.FormatUint(uint64(value), 10), nil
	}
	return strconv.FormatInt(int64(value), 10), nil
}

// ModelIDPath preserves model ownership when decoding and generating URLs. It
// rejects the nil UUID. Parsing is not a database lookup or authorization check.
func ModelIDPath[M any]() PathCodec[model.ID[M]] {
	return nativeURLCodec[model.ID[M]](modelIDPathCodec[M]{}, contract.Type{Kind: contract.StringKind, Format: contract.UUIDFormat}, ModelIDURLSyntax)
}

type modelIDPathCodec[M any] struct{}

func (modelIDPathCodec[M]) Parse(text string) (model.ID[M], error) {
	id, err := model.ParseID[M](text)
	if err != nil {
		return model.ID[M]{}, err
	}
	if id.IsZero() {
		return id, fault.New(fault.Invalid, "model path identity is empty")
	}
	return id, nil
}

func (modelIDPathCodec[M]) Format(id model.ID[M]) (string, error) {
	if id.IsZero() {
		return "", fault.New(fault.Invalid, "model path identity is empty")
	}
	return id.String(), nil
}
