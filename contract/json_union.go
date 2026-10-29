package contract

import (
	"context"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// UnionValue is immutable generated-union storage, scoped to its concrete union.
// A validated wire snapshot prevents mutable payload aliases and recursive custom
// encoders from changing a previously constructed union. Zero is invalid.
// Consumers use generated constructors/accessors, never a dynamic variant bag.
type UnionValue[T any] struct {
	_                  [0]*T
	wire, payload, tag string
}

// Tag returns the selected literal, or empty for an unset union.
func (v UnionValue[T]) Tag() string  { return v.tag }
func (v UnionValue[T]) IsZero() bool { return v.wire == "" }
func (v UnionValue[T]) MarshalJSON() ([]byte, error) {
	if v.IsZero() {
		return nil, invalidUnion()
	}
	return []byte(v.wire), nil
}
func invalidUnion() error { return safeerror.NewFault(fault.Invalid, "invalid union value") }
func invalidUnionCause(err error) error {
	return safeerror.WrapFault(fault.Invalid, "invalid union value", safeerror.Diagnostic("union JSON codec failed", err))
}

// Union helper bounds also protect direct encoding/json use. An HTTP descriptor
// additionally applies its configured, usually smaller, complete-document bounds.
func unionLimits() JSONLimits {
	return JSONLimits{Bytes: jsonwire.MaxBytes, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes, Steps: jsonwire.MaxNodes * 4, Issues: 16}
}

// EncodeUnion captures an owned typed variant once. Its generated caller chooses
// the literal; the shared graph rejects undeclared tags and incorrect wire shapes.
// Union payload accessors decode owned values, like value.JSON's snapshot API.
func EncodeUnion[T, P any](descriptor JSON[T], tag string, input P) (UnionValue[T], error) {
	if err := descriptor.Validate(); err != nil {
		return UnionValue[T]{}, err
	}
	root := descriptor.schema.types[descriptor.schema.description.Root]
	if root.Kind != UnionKind || descriptor.schema.variants[root.ID][tag] == "" {
		return UnionValue[T]{}, invalidUnion()
	}
	limits := unionLimits()
	payload, err := value.EncodeJSON(context.Background(), input, value.JSONEncodingLimits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes, Steps: limits.Steps})
	if err != nil {
		return UnionValue[T]{}, invalidUnionCause(err)
	}
	if len(payload) < 2 || payload[0] != '{' || payload[len(payload)-1] != '}' {
		return UnionValue[T]{}, invalidUnion()
	}
	name, _ := json.Marshal(root.Discriminator)
	literal, _ := json.Marshal(tag)
	size := len(name) + len(literal) + len(payload) + 2
	if size > limits.Bytes {
		return UnionValue[T]{}, invalidUnion()
	}
	wire := make([]byte, 0, size)
	wire = append(wire, '{')
	wire = append(wire, name...)
	wire = append(wire, ':')
	wire = append(wire, literal...)
	if len(payload) > 2 {
		wire = append(wire, ',')
		wire = append(wire, payload[1:len(payload)-1]...)
	}
	wire = append(wire, '}')
	return DecodeUnion(descriptor, wire)
}

// DecodeUnion validates the complete tagged wire object without calling T's
// decoder recursively. Generated code then checks the selected native Go payload.
func DecodeUnion[T any](descriptor JSON[T], data []byte) (UnionValue[T], error) {
	if err := descriptor.Validate(); err != nil {
		return UnionValue[T]{}, err
	}
	snapshot, err := decodeUnionSnapshot(descriptor.schema, data)
	return UnionValue[T]{wire: snapshot.wire, payload: snapshot.payload, tag: snapshot.tag}, err
}

// UnionVariant returns a fresh concrete payload. It does not expose or share the
// stored snapshot. Generated accessors supply the exact payload and literal.
func UnionVariant[T, P any](stored UnionValue[T], tag string) (P, error) {
	var result P
	if stored.IsZero() || stored.tag != tag {
		return result, invalidUnion()
	}
	err := callback.Isolated("decode union variant", func() error { return decodeNativeJSON([]byte(stored.payload), &result) })
	if err != nil {
		// Keep the shared JSON boundary's panic/Goexit classification. Ordinary
		// codec errors remain value-free; never call arbitrary error methods.
		if safeerror.IsPanic(err) {
			return *new(P), err
		}
		return *new(P), invalidUnionCause(err)
	}
	return result, nil
}

// unionSnapshot is validated and immutable before entering a typed union shell.
type unionSnapshot struct{ wire, payload, tag string }

func decodeUnionSnapshot(schema *compiledSchema, data []byte) (unionSnapshot, error) {
	root := schema.types[schema.description.Root]
	if root.Kind != UnionKind {
		return unionSnapshot{}, invalidUnion()
	}
	limits := unionLimits()
	node, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if err != nil {
		return unionSnapshot{}, decodeFailure(nil, err)
	}
	issues, err := schema.check(context.Background(), node, shapeLimits{steps: limits.Steps, issues: limits.Issues})
	if err != nil || len(issues) != 0 {
		return unionSnapshot{}, decodeFailure(issues, err)
	}
	object, ok := node.(map[string]any)
	if !ok {
		return unionSnapshot{}, invalidUnion()
	}
	tag, ok := object[root.Discriminator].(string)
	if !ok {
		return unionSnapshot{}, invalidUnion()
	}
	delete(object, root.Discriminator)
	payload, err := json.Marshal(object)
	if err != nil || len(payload) > limits.Bytes {
		return unionSnapshot{}, invalidUnion()
	}
	return unionSnapshot{wire: string(data), payload: string(payload), tag: tag}, nil
}
