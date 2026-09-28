package http

import "github.com/weiloon1234/Foundry-Go/model"

// QueryCodec converts one decoded query value. Query and path values share the
// same scalar conversion contract; their escaping and structural rules differ.
type QueryCodec[V any] = PathCodec[V]

// QueryInteger retains ordinary or named integer types through query binding.
type QueryInteger = PathInteger

// QueryFloat retains ordinary or named floating-point types through query binding.
type QueryFloat = PathFloat

// QueryTextPointer is the complete text codec of the concrete value V.
type QueryTextPointer[V any] = PathTextPointer[V]

// StringQuery preserves text, including spaces and explicit empty values.
func StringQuery[V ~string]() QueryCodec[V] { return StringPath[V]() }

// IntegerQuery requires canonical decimal input and preserves the integer width.
func IntegerQuery[V QueryInteger]() QueryCodec[V] { return IntegerPath[V]() }

// FloatQuery accepts finite decimal/scientific input at the concrete float width.
// Use a text codec such as decimal.Decimal for exact decimal representations.
func FloatQuery[V QueryFloat]() QueryCodec[V] { return FloatPath[V]() }

// BoolQuery uses exactly true and false. A bare query key is not an implicit true.
func BoolQuery[V ~bool]() QueryCodec[V] { return BoolPath[V]() }

// ModelIDQuery preserves model ownership and rejects an empty model identity.
// Parsing is not a database lookup or an authorization check.
func ModelIDQuery[M any]() QueryCodec[model.ID[M]] { return ModelIDPath[M]() }

// TextQuery reuses the value's text methods, including generated enum membership
// validation and exact decimal/temporal representations. Custom methods must be
// deterministic, concurrency-safe and return owned values and bounded output.
func TextQuery[V any, P QueryTextPointer[V]]() QueryCodec[V] { return TextPath[V, P]() }
