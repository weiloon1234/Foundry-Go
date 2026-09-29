package query

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSONPath retains a row's owner and the Go payload at a generated JSON path.
// Prefer generated Properties and At methods over declaration constructors.
type JSONPath[S, P any] struct {
	expression Expression[S, value.Nullable[value.JSON[P]]]
}

// JSONRoot starts a typed path at a non-nullable row document.
func JSONRoot[S, P any](input RowValue[S, value.JSON[P]]) JSONPath[S, P] {
	return JSONPath[S, P]{Nullable(rowInput(input).Value())}
}

// JSONNullableRoot starts a typed path while preserving a nullable SQL column.
func JSONNullableRoot[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) JSONPath[S, P] {
	return JSONPath[S, P]{rowInput(input).Value()}
}

// JSON returns a nullable snapshot of the declared payload. SQL NULL denotes
// an absent path or SQL-null ancestor; a present JSON null remains JSON null.
// A JSON null incompatible with P fails decoding, just like a whole document.
func (p JSONPath[S, P]) JSON() RowExpression[S, value.Nullable[value.JSON[P]]] {
	return RowExpression[S, value.Nullable[value.JSON[P]]]{p.expression}
}
func (p JSONPath[S, P]) Kind() RowExpression[S, value.Nullable[JSONKind]] {
	return JSONTypeNullable(p.JSON())
}

// Exists includes a present JSON null. A missing or SQL-null ancestor is absent.
func (p JSONPath[S, P]) Exists() Predicate[S]     { return p.JSON().IsNotNull() }
func (p JSONPath[S, P]) IsMissing() Predicate[S]  { return p.JSON().IsNull() }
func (p JSONPath[S, P]) IsJSONNull() Predicate[S] { return p.Kind().Eq(value.Of(JSONNull)) }

// NewJSONProperty is a generator declaration boundary. Name is a JSON wire
// property, never SQL. Quoted decodes a declared encoding/json ,string field.
// The declared name is rendered as an escaped SQL literal, so a predicate on
// (document -> 'name') can use an expression index on the same path.
func NewJSONProperty[S, P, C any](parent JSONPath[S, P], name string, quoted bool) JSONPath[S, C] {
	return jsonProperty[S, P, C](parent, name, quoted, true)
}

// jsonProperty binds run-time map keys as parameters and declared names as literals.
func jsonProperty[S, P, C any](parent JSONPath[S, P], name string, quoted, literal bool) JSONPath[S, C] {
	result := jsonPathOperation[S, C](jsonPropertyOperation, operationArg(parent.expression), operationArg(jsonPathKey[S](name, literal).Value()))
	if quoted {
		result = jsonPathOperation[S, C](jsonUnquoteOperation, operationArg(result.expression))
	}
	return result
}

// NewJSONArrayElement is a generator declaration boundary for array elements.
// Indices start at zero; negative indices count from the end, as in PostgreSQL.
// The index is a run-time value, so it stays a bind parameter.
func NewJSONArrayElement[S, P, C any](parent JSONPath[S, P], index int32) JSONPath[S, C] {
	return jsonPathOperation[S, C](jsonIndexOperation, operationArg(parent.expression), operationArg(parameterExpression[S](index, codec.Signed[int32]()).Value()))
}

// NewJSONMapEntry is a generator declaration boundary retaining the declared
// map-key type. It uses the JSON value codec's canonical key validation.
func NewJSONMapEntry[S, P, C any, K comparable](parent JSONPath[S, P], key K) JSONPath[S, C] {
	encoded, err := value.NewJSON(map[K]bool{key: true})
	var name string
	if err == nil {
		var text string
		text, err = encoded.Text()
		if err == nil {
			var object map[string]json.RawMessage
			err = json.Unmarshal([]byte(text), &object)
			if err == nil && len(object) != 1 {
				err = fault.New(fault.Invalid, "JSON map key must encode one property")
			}
			for k := range object {
				name = k
			}
		}
	}
	if err != nil {
		return JSONPath[S, C]{Expression[S, value.Nullable[value.JSON[C]]]{node: parameterNode{err: fault.New(fault.Invalid, "invalid JSON map key")}, codec: codec.Nullable(codec.JSON[C]())}}
	}
	return jsonProperty[S, P, C](parent, name, false, false)
}

// jsonPathKey renders a declared property name as a SQL literal: the path
// (document -> 'name') then matches an expression index on the same path even
// under generic plans. Run-time map keys remain bind parameters.
func jsonPathKey[S any](name string, literal bool) RowExpression[S, string] {
	result := parameterExpression[S](name, codec.String[string]())
	if node, ok := result.expression.node.(parameterNode); ok && literal {
		node.literal = true
		result.expression.node = node
	}
	if len(name) > value.JSONMaxBytes || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		result.expression.node = parameterNode{err: fault.New(fault.Invalid, "invalid JSON property name")}
	}
	return result
}

func jsonPathOperation[S, P any](op scalarOperation, args ...operationArgument) JSONPath[S, P] {
	return JSONPath[S, P]{operationValue[S](op, codec.Nullable(codec.JSON[P]()), args...)}
}

// JSONScalar extracts a declared scalar through its SQL codec. Missing paths
// and JSON null both produce SQL NULL; JSON and Kind retain the distinction.
// This is a generated declaration boundary, not a free-form SQL conversion.
func JSONScalar[S, P, V any](path JSONPath[S, P], c codec.Codec[V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{operationValue[S](jsonScalarOperation, codec.Nullable(c), operationArg(path.expression))}
}

// JSONOrderedScalar adds comparisons for a generated ordered payload type.
func JSONOrderedScalar[S, P any, V orderedScalar](path JSONPath[S, P], c codec.Codec[V]) NullableOrderedRowExpression[S, V] {
	return OrderNullableRow(JSONScalar(path, c))
}

// JSONTextScalar adds LIKE and literal substring filters for declared text.
func JSONTextScalar[S, P any, V ~string](path JSONPath[S, P], c codec.Codec[V]) NullableTextRowExpression[S, V] {
	return NullableTextRowExpression[S, V]{JSONOrderedScalar(path, c)}
}
