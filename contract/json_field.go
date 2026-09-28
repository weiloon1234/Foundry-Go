package contract

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/gotype"
)

// DefineJSONField declares an explicitly JSON-valued Go field as an independent
// document. Generated multipart declarations use it for scalars, ordinary/named
// collections, nullable values, custom codecs and structured fields. Complete
// public DTO declarations continue to use DefineJSON.
//
// Like DefineScalar, this is a metadata construction boundary: generation or
// the declaration author owns agreement between T, its native serialization and
// this graph. It does not infer or duplicate a schema from reflection. It shares
// schema compilation, limits and all-or-zero decoding with every JSON contract.
// Persistence-model roots are rejected without invoking their identity methods;
// generation also rejects nested models and unsupported native representations.
func DefineJSONField[T any](description Schema) JSON[T] {
	if jsonModelType(reflect.TypeFor[T]()) {
		return JSON[T]{err: invalidSchema()}
	}
	schema, err := compileSchema(description)
	result := JSON[T]{schema: schema, err: err}
	if jsonDeclarationMatches(reflect.TypeFor[T](), description.Root) {
		result.sourceName = gotype.Source(string(description.Root))
	}
	return result
}
