package contract

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/gotype"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// DefineJSONValue describes the wire representation of a named custom value.
// Native Go JSON/text methods own serialization; this declaration owns the shape
// checked before decoding and after encoding. Ordinary DTOs use DefineJSON.
// T must provide a value encoder and a supported native JSON or text decoder on *T.
// A pointer-only encoder is insufficient because map elements and top-level
// values are not addressable under the framework's native JSON semantics.
// The descriptor cannot describe a persistence model as a public DTO.
func DefineJSONValue[T any](description Schema) JSON[T] {
	typ := reflect.TypeFor[T]()
	if !jsonDeclarationMatches(typ, description.Root) ||
		!jsonshape.HasEncoder(typ, false) ||
		!jsonshape.HasDecoder(typ, true) {
		return JSON[T]{err: invalidSchema()}
	}
	schema, err := compileSchema(description)
	return JSON[T]{schema: schema, err: err, sourceName: gotype.Source(string(description.Root))}
}

// JSONType includes a custom value's typed contract in a generated DTO graph.
// The factory must describe T using the exact supplied qualified identity. It is
// called once during descriptor construction, never while decoding a request.
// A runtime main package (including a named generic argument) keeps its source
// identity through a generated alias; other qualified names must match exactly.
// Factories must be deterministic, bounded and independent of receiver state.
// Failure, panic, Goexit, conflicting definitions and an invalid zero descriptor
// reject the containing contract. Generated declarations supply this boundary.
func JSONType[T any](id TypeID, factory func() JSON[T]) Type {
	return Type{ID: id, jsonContract: func() (Schema, error) {
		if factory == nil {
			return Schema{}, invalidSchema()
		}
		description, err := factory().Description()
		if err != nil {
			return Schema{}, invalidSchema()
		}
		if description.Root != id {
			typ := reflect.TypeFor[T]()
			if !jsonDeclarationMatches(typ, id) || !jsonDeclarationMatches(typ, description.Root) {
				return Schema{}, invalidSchema()
			}
			// Retain the factory's immutable graph and native runtime identity;
			// its generated alias records the executable's source namespace.
			description.Types = append(description.Types, Type{ID: id, Kind: AliasKind, Element: description.Root})
			description.Root = id
		}
		return description, nil
	}}
}
