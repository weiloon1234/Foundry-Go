package expr

import (
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSONPath retains its row owner and exact payload without embedded method chains.
// Example: JSONProperty(JSONRoot(Value(Documents.Settings)), Theme).
type JSONPath[S, P any] struct {
	_    [0]*S
	_    [0]*P
	data rowData[value.Nullable[value.JSON[P]]]
}

// JSONRoot starts a row path; selected expressions cannot become path predicates.
// Example: expr.JSONRoot(Value(Documents.Settings)).
func JSONRoot[S, P any](input RowValue[S, value.JSON[P]]) JSONPath[S, P] {
	return JSONPath[S, P]{data: nullableData(rowInput(input).data)}
}

// JSONNullableRoot preserves a SQL-null document ancestor.
// Example: expr.JSONNullableRoot(Value(Documents.Backup)).
func JSONNullableRoot[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) JSONPath[S, P] {
	return JSONPath[S, P]{data: rowInput(input).data}
}

// JSON selects a nullable payload snapshot. Missing/SQL-null ancestors are SQL NULL;
// present JSON null is decoded through P and can fail if P cannot represent it.
// Example: path.JSON().Value().
func (p JSONPath[S, P]) JSON() RowExpression[S, value.Nullable[value.JSON[P]]] {
	return RowExpression[S, value.Nullable[value.JSON[P]]]{data: p.data}
}

// JSONProperty applies a startup-validated field declaration. The json tag is a
// SQL literal for expression-index parity; query-time map keys remain parameters.
// Example: expr.JSONProperty(root, Theme).
func JSONProperty[S, P, V any](parent JSONPath[S, P], field JSONField[P, V]) JSONPath[S, V] {
	arg := jsonKeyArgument(field.binding.Name, true, field.Validate())
	result := jsonPathOperation[S, V](querycore.JsonPropertyOperation, jsonPathArg(parent), arg)
	if field.binding.Quoted {
		result = jsonPathOperation[S, V](querycore.JsonUnquoteOperation, jsonPathArg(result))
	}
	return result
}

// JSONIndex selects a typed slice element. Negative int32 indices count from the end.
// Example: expr.JSONIndex(scores, -1).
func JSONIndex[S any, P ~[]V, V any](parent JSONPath[S, P], index int32) JSONPath[S, V] {
	arg := integerArgument(index)
	arg.Value.Err = errors.Join(arg.Value.Err, ormcore.JSONCollection(reflect.TypeFor[P]()))
	return jsonPathOperation[S, V](querycore.JsonIndexOperation, jsonPathArg(parent), arg)
}

// JSONMapKey preserves the map's declared key and element types, including named keys.
// Example: expr.JSONMapKey(labels, "language").
func JSONMapKey[S any, P ~map[K]V, K comparable, V any](parent JSONPath[S, P], key K) JSONPath[S, V] {
	name, err := querycore.JSONMapKey(key)
	return jsonPathOperation[S, V](querycore.JsonPropertyOperation, jsonPathArg(parent), jsonKeyArgument(name, false, errors.Join(err, ormcore.JSONCollection(reflect.TypeFor[P]()))))
}

// JSONArrayElement selects a fixed array element using its validated declaration.
// Example: expr.JSONArrayElement(fixed, Elements, 1).
func JSONArrayElement[S, P, V any](parent JSONPath[S, P], array JSONFixedArray[P, V], index int32) JSONPath[S, V] {
	arg := integerArgument(index)
	arg.Value.Err = errors.Join(arg.Value.Err, array.Validate())
	return jsonPathOperation[S, V](querycore.JsonIndexOperation, jsonPathArg(parent), arg)
}

func jsonKeyArgument(name string, literal bool, err error) ormcore.OperationArgument {
	err = errors.Join(err, querycore.ValidateJSONProperty(name))
	arg := textOperationArgument(name, err)
	node := arg.Value.Node.(querycore.ParameterNode)
	node.Literal = literal
	arg.Value.Node = node
	return arg
}
func jsonPathOperation[S, P any](kind querycore.ScalarOperation, args ...ormcore.OperationArgument) JSONPath[S, P] {
	return JSONPath[S, P]{data: operationValue[S](kind, columnCodec(codec.Nullable(codec.JSON[P]())), args...).data}
}

// JSONScalar uses the exact payload codec. Missing paths and JSON null become SQL NULL.
// Example: expr.JSONScalar(flag, codec.Bool[bool]()).
func JSONScalar[S, P any](path JSONPath[S, P], c codec.Codec[P]) RowExpression[S, value.Nullable[P]] {
	return RowExpression[S, value.Nullable[P]]{data: operationValue[S](querycore.JsonScalarOperation, columnCodec(codec.Nullable(c)), jsonPathArg(path)).data}
}

// JSONOrderedScalar adds comparisons only for ordered payloads.
// Example: expr.JSONOrderedScalar(age, codec.Signed[int64]()).
func JSONOrderedScalar[S any, P orderedScalar](path JSONPath[S, P], c codec.Codec[P]) NullableOrderedRowExpression[S, P] {
	return OrderNullableRow(JSONScalar(path, c))
}

// JSONTextScalar adds pattern predicates only for string payloads.
// Example: expr.JSONTextScalar(name, codec.String[string]()).
func JSONTextScalar[S any, P ~string](path JSONPath[S, P], c codec.Codec[P]) NullableTextRowExpression[S, P] {
	return NullableTextRowExpression[S, P]{data: JSONScalar(path, c).data}
}

// JSONOptional unwraps property presence into path presence; no SQL cast is added.
// Example: expr.JSONOptional(JSONProperty(root, Theme)).
func JSONOptional[S, P any](path JSONPath[S, value.Optional[P]]) JSONPath[S, P] {
	return jsonRebind[S, P](path.data.core)
}

// JSONNullable unwraps payload nullability; JSON/Kind still distinguish JSON null.
// Example: expr.JSONNullable(state).
func JSONNullable[S, P any](path JSONPath[S, value.Nullable[P]]) JSONPath[S, P] {
	return jsonRebind[S, P](path.data.core)
}

// JSONPointer unwraps a pointer payload for typed traversal or scalar extraction.
// Example: expr.JSONPointer(next).
func JSONPointer[S, P any](path JSONPath[S, *P]) JSONPath[S, P] {
	return jsonRebind[S, P](path.data.core)
}

// JSONDocument unwraps a nested JSON value's payload without changing its SQL node.
// Example: expr.JSONDocument(nested).
func JSONDocument[S, P any](path JSONPath[S, value.JSON[P]]) JSONPath[S, P] {
	return jsonRebind[S, P](path.data.core)
}
func jsonRebind[S, P any](core ormcore.Expression) JSONPath[S, P] {
	c := columnCodec(codec.Nullable(codec.JSON[P]()))
	if core.Node == nil {
		core.Err = errors.Join(core.Err, ormcore.Invalid("uninitialized JSON path"))
	}
	core.Err = errors.Join(core.Err, c.Err)
	return JSONPath[S, P]{data: rowData[value.Nullable[value.JSON[P]]]{core: core, column: c}}
}

func jsonPathArg[S, P any](p JSONPath[S, P]) ormcore.OperationArgument {
	return ormcore.OperationArgument{Value: p.data.core, Kind: p.data.column.Kind}
}
