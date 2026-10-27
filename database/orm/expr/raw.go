package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Raw declares a selected-phase SQL fragment with a built-in result codec.
// Use ? for bound arguments and ?? for a literal question mark. The caller owns
// SQL semantics and result type; scopes and transactions belong to the query.
// Example: expr.Raw[User,int64]("COALESCE(score, ?)", 0).
func Raw[S, V any](sql string, args ...any) Selected[S, V] {
	return RawWithCodec[S](ormcore.ResultCodec[V](), sql, args...)
}

// RawWithCodec supplies an explicit result codec, including custom/nullable types.
// Example: expr.RawWithCodec[User](codec.Nullable(codec.String[Label]()),"label").
func RawWithCodec[S, V any](c codec.Codec[V], sql string, args ...any) Selected[S, V] {
	node := querycore.NewRawFragment(sql, args)
	column := columnCodec(c)
	return Selected[S, V]{data: rowData[V]{column: column, core: ormcore.Expression{Node: node, ReferenceFree: true, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[S]()}}, Err: safeerror.Join("operation failed", node.Validate(), column.Err)}}}
}
