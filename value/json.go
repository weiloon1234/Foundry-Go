package value

import (
	jsonv2 "encoding/json/v2"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const (
	JSONMaxBytes        = jsonwire.MaxBytes
	JSONMaxDepth        = jsonwire.MaxDepth
	JSONMaxNodes        = jsonwire.MaxNodes
	JSONMaxNumberDigits = decimal.MaxDigits
)

// JSON stores an immutable, typed JSON snapshot. Decode returns fresh values,
// including fresh maps/slices. Canonical equality matches JSONB value equality;
// object key order and numeric spelling are not retained. The zero value is
// invalid, not JSON null or SQL NULL. Use JSON[Nullable[T]] for JSON null and
// Nullable[JSON[T]] for a nullable SQL column.
type JSON[T any] struct {
	_    [0]*T
	text string
}

// NewJSON snapshots a concrete Go value through its JSON representation.
// Custom JSON/text methods own their validation and must be deterministic and
// safe for concurrent use, preserve inputs and return owned decoded values.
// Construction retains only canonical text, never the input's maps or slices.
func NewJSON[T any](input T) (JSON[T], error) {
	budget := jsonInputBudget{}
	if err := budget.check(reflect.ValueOf(input), 0); err != nil {
		return JSON[T]{}, err
	}
	data, err := jsoncodec.Marshal(input)
	if err != nil {
		return JSON[T]{}, invalidJSONCause(err)
	}
	return ParseJSON[T](string(data))
}

// ParseJSON validates JSON syntax, resource bounds and its concrete Go shape.
// Unknown/duplicate keys, missing required struct fields, invalid Unicode and
// nulls in non-nullable fields fail. Optional, omitempty and omitzero fields may
// be absent. Custom unmarshalers own the schema inside their representation.
func ParseJSON[T any](text string) (JSON[T], error) {
	if len(text) == 0 || len(text) > JSONMaxBytes {
		return JSON[T]{}, invalidJSON()
	}
	canonical, node, err := jsonwire.Parse([]byte(text))
	if err != nil {
		return JSON[T]{}, err
	}
	if err := validateJSONShape(node, reflect.TypeFor[T](), 0); err != nil {
		return JSON[T]{}, err
	}
	if _, err := decodeJSON[T](canonical); err != nil {
		return JSON[T]{}, err
	}
	return JSON[T]{text: canonical}, nil
}

func invalidJSONCause(err error) error {
	return safeerror.WrapFault(fault.Invalid, "invalid typed JSON value or schema", safeerror.Diagnostic("JSON codec failed", err))
}

func invalidJSON() error {
	return safeerror.NewFault(fault.Invalid, "invalid typed JSON value or schema")
}
func (v JSON[T]) IsZero() bool     { return v.text == "" }
func (v JSON[T]) IsJSONNull() bool { return v.text == "null" }

// Text returns canonical JSON for an explicit transport/codec boundary.
func (v JSON[T]) Text() (string, error) {
	if v.IsZero() {
		return "", invalidJSON()
	}
	return v.text, nil
}

// Decode creates an independent T. Dynamic interface values use json.Number.
func (v JSON[T]) Decode() (T, error) {
	if v.IsZero() {
		return *new(T), invalidJSON()
	}
	return decodeJSON[T](v.text)
}
func decodeJSON[T any](text string) (T, error) {
	var result T
	if err := jsoncodec.UnmarshalExact([]byte(text), &result, jsonv2.RejectUnknownMembers(true)); err != nil {
		return *new(T), invalidJSONCause(err)
	}
	return result, nil
}
func (v JSON[T]) MarshalJSON() ([]byte, error) {
	text, err := v.Text()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}
func (v *JSON[T]) UnmarshalJSON(data []byte) error {
	if v == nil || len(data) == 0 || len(data) > JSONMaxBytes {
		return invalidJSON()
	}
	decoded, err := ParseJSON[T](string(data))
	if err != nil {
		return jsoncodec.MethodError(err)
	}
	*v = decoded
	return nil
}

func (JSON[T]) jsonContentType() reflect.Type      { return reflect.TypeFor[T]() }
func (Nullable[T]) jsonNullableType() reflect.Type { return reflect.TypeFor[T]() }
func (Optional[T]) jsonOptionalType() reflect.Type { return reflect.TypeFor[T]() }

func (v Nullable[T]) jsonInput() reflect.Value {
	if v.IsNull() {
		return reflect.Value{}
	}
	return reflect.ValueOf(v.value)
}
func (v Optional[T]) jsonInput() reflect.Value {
	if !v.IsSet() {
		return reflect.Value{}
	}
	return reflect.ValueOf(v.value)
}
