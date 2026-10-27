package pagination

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorRequest retains the original model/projection owner in its ORM request.
// After and Before contain cursor positions, not credentials or authorization.
type CursorRequest[P, F, M any] = pageRequest[P, F, orm.CursorRequest[M]]

type cursorCodec[M any] struct{}

func (cursorCodec[M]) Parse(text string) (orm.Cursor[M], error) { return orm.ParseCursor[M](text) }
func (cursorCodec[M]) Format(cursor orm.Cursor[M]) (string, error) {
	if err := cursor.Validate(); err != nil {
		return "", err
	}
	return cursor.Token(), nil
}

// CursorQuery supplies the same typed, bounded cursor codec to custom query
// declarations. Its scalar metadata preserves the original model/result owner.
// Successful parsing validates structure; query execution verifies scope.
func CursorQuery[M any]() foundryhttp.QueryCodec[orm.Cursor[M]] {
	// This published wire identity is independent of the shared Go implementation owner.
	id := contract.TypeID("github.com/weiloon1234/Foundry-Go/database/orm." + reflect.TypeFor[orm.Cursor[M]]().Name())
	return foundryhttp.DescribeURL[orm.Cursor[M]](cursorCodec[M]{}, contract.DefineScalar[orm.Cursor[M]](contract.Type{ID: id, Kind: contract.StringKind}))
}

func cursorParameters[F, M any](filters foundryhttp.Query[F], config CursorConfig) foundryhttp.Query[parameters[F, orm.CursorRequest[M]]] {
	codec := CursorQuery[M]()
	return foundryhttp.MergeQueries(
		foundryhttp.EmbedQuery(filters, func(in *parameters[F, orm.CursorRequest[M]]) *F { return &in.Filters }),
		foundryhttp.DefineQuery(
			foundryhttp.OptionalQueryParam(config.AfterParam, codec, func(in *parameters[F, orm.CursorRequest[M]]) *value.Optional[orm.Cursor[M]] {
				return &in.Page.After
			}),
			foundryhttp.OptionalQueryParam(config.BeforeParam, codec, func(in *parameters[F, orm.CursorRequest[M]]) *value.Optional[orm.Cursor[M]] {
				return &in.Page.Before
			}),
			foundryhttp.DefaultQueryParam(config.SizeParam, foundryhttp.IntegerQuery[int](), config.DefaultSize, func(in *parameters[F, orm.CursorRequest[M]]) *int { return &in.Page.Size }).WithPresentation(labelPresentation(SizeLabelKey)),
		),
	)
}
func cursorRules[F, M any](config CursorConfig) validation.Rule[parameters[F, orm.CursorRequest[M]]] {
	size := validation.DefineField(config.SizeParam, func(in orm.CursorRequest[M]) int { return in.Size }).WithLabel(sizeLabel).WithLabelKey(SizeLabelKey)
	valid := validation.Custom[orm.CursorRequest[M]](validation.Spec{ID: "pagination.cursor_request", Message: "Supply at most one valid cursor direction."}, func(_ context.Context, in orm.CursorRequest[M]) (bool, error) { return in.Validate() == nil, nil })
	return validation.Embed(validation.Bail(size.Rules(validation.Min(1), validation.Max(config.MaximumSize)), valid), func(in parameters[F, orm.CursorRequest[M]]) orm.CursorRequest[M] { return in.Page })
}
