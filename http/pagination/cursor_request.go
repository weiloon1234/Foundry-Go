package pagination

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/contractmeta"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorRequest retains the original model/projection owner in its ORM request.
// After and Before contain cursor positions, not credentials or authorization.
type CursorRequest[P, F, M any] = pageRequest[P, F, query.CursorRequest[M]]

type cursorCodec[M any] struct{}

func (cursorCodec[M]) Parse(text string) (query.Cursor[M], error) { return query.ParseCursor[M](text) }
func (cursorCodec[M]) Format(cursor query.Cursor[M]) (string, error) {
	if err := cursor.Validate(); err != nil {
		return "", err
	}
	return cursor.Token(), nil
}

// CursorQuery supplies the same typed, bounded cursor codec to custom query
// declarations. Its scalar metadata preserves the original model/result owner.
// Successful parsing validates structure; query execution verifies scope.
func CursorQuery[M any]() foundryhttp.QueryCodec[query.Cursor[M]] {
	return foundryhttp.DescribeURL[query.Cursor[M]](cursorCodec[M]{}, contract.DefineScalar[query.Cursor[M]](contract.Type{ID: contractmeta.TypeID(reflect.TypeFor[query.Cursor[M]]()), Kind: contract.StringKind}))
}

func cursorParameters[F, M any](filters foundryhttp.Query[F], config CursorConfig) foundryhttp.Query[parameters[F, query.CursorRequest[M]]] {
	codec := CursorQuery[M]()
	return foundryhttp.MergeQueries(
		foundryhttp.EmbedQuery(filters, func(in *parameters[F, query.CursorRequest[M]]) *F { return &in.Filters }),
		foundryhttp.DefineQuery(
			foundryhttp.OptionalQueryParam(config.AfterParam, codec, func(in *parameters[F, query.CursorRequest[M]]) *value.Optional[query.Cursor[M]] {
				return &in.Page.After
			}),
			foundryhttp.OptionalQueryParam(config.BeforeParam, codec, func(in *parameters[F, query.CursorRequest[M]]) *value.Optional[query.Cursor[M]] {
				return &in.Page.Before
			}),
			foundryhttp.DefaultQueryParam(config.SizeParam, foundryhttp.IntegerQuery[int](), config.DefaultSize, func(in *parameters[F, query.CursorRequest[M]]) *int { return &in.Page.Size }),
		),
	)
}
func cursorRules[F, M any](config CursorConfig) validation.Rule[parameters[F, query.CursorRequest[M]]] {
	size := validation.DefineField(config.SizeParam, func(in query.CursorRequest[M]) int { return in.Size })
	valid := validation.Custom[query.CursorRequest[M]](validation.Spec{ID: "pagination.cursor_request", Message: "Supply at most one valid cursor direction."}, func(_ context.Context, in query.CursorRequest[M]) (bool, error) { return in.Validate() == nil, nil })
	return validation.Embed(validation.Bail(size.Rules(validation.Min(1), validation.Max(config.MaximumSize)), valid), func(in parameters[F, query.CursorRequest[M]]) query.CursorRequest[M] { return in.Page })
}
