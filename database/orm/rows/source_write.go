package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MapInsert maps an exact stored SQL value without running a mutator. Example: rows.MapInsert(Archives.Name,expr.Value(Users.Name)).
func MapInsert[S, M, V, P any, C orm.InsertTarget[M, V, P]](field C, expression expr.RowValue[S, P]) orm.InsertMapping[S, M] {
	return orm.FoundryInsertMapping[S, M](modelMapping(field, expression), sqlowner.Expressions())
}

// MapUpdate maps an exact stored SQL value to a non-primary field. Example: rows.MapUpdate(Users.Name,expr.Value(Archives.Name)).
func MapUpdate[S, M, V, P any, C orm.UpdateTarget[M, V, P]](field C, expression expr.RowValue[S, P]) orm.UpdateMapping[S, M] {
	return orm.FoundryUpdateMapping[S, M](modelMapping(field, expression), sqlowner.Expressions())
}

// MatchSource matches compatible stored primary values without assigning them. Example: rows.MatchSource(Users.ID,expr.Value(Archives.UserID)).
func MatchSource[S, M, V, P any](field orm.ExpressionColumn[M, V, P], expression expr.RowValue[S, P]) orm.SourceKey[S, M] {
	return orm.FoundrySourceKey[S, M](modelMapping(field, expression), sqlowner.Expressions())
}

// MatchNullableSource accepts outer-join keys; NULL matches no destination. Example: rows.MatchNullableSource(Users.ID,nullableUserID).
func MatchNullableSource[S, M, V, P any](field orm.ExpressionColumn[M, V, P], expression expr.RowValue[S, value.Nullable[P]]) orm.SourceKey[S, M] {
	_, c := orm.FoundryColumn(field, sqlowner.Metadata())
	return orm.FoundrySourceKey[S, M](ormcore.ModelMapping{Destination: c.Reference, Field: c.Field, Stored: reflect.TypeFor[P](), Value: expr.FoundryRowValue(expression, sqlowner.Expressions()), NullableKey: true, Err: c.Err}, sqlowner.Expressions())
}
func modelMapping[S, M, V, P any](field orm.ExpressionColumn[M, V, P], expression expr.RowValue[S, P]) ormcore.ModelMapping {
	_, c := orm.FoundryColumn(field, sqlowner.Metadata())
	return ormcore.ModelMapping{Destination: c.Reference, Field: c.Field, Stored: reflect.TypeFor[P](), Value: expr.FoundryRowValue(expression, sqlowner.Expressions()), Err: c.Err}
}

// InsertFrom inserts the selected source window, keeping defaults and NULL for omitted fields. Example: rows.InsertFrom(rows.From(Users.Query()),Archives.Query(),mapping).
func InsertFrom[S, M any](source SelectionSource[S], destination orm.Query[M], mappings ...orm.InsertMapping[S, M]) orm.InsertSelect[S, M] {
	return orm.FoundryInsertFrom[S](destination, sourceOf(source), sqlowner.Queries()).Select(mappings...)
}

// UpdateFrom preserves source windows and destination scopes independently. Example: rows.UpdateFrom(Users.Query(),rows.From(Archives.Query())).Match(key).
func UpdateFrom[S, M any](destination orm.Query[M], source SelectionSource[S]) orm.UpdateSource[S, M] {
	return orm.FoundryUpdateFrom[S](destination, sourceOf(source), sqlowner.Queries())
}

// DeleteUsing applies ordinary soft deletion and deduplicates source matches. Example: rows.DeleteUsing(Users.Query(),rows.From(Archives.Query())).Match(key).
func DeleteUsing[S, M any](destination orm.Query[M], source SelectionSource[S]) orm.DeleteSource[S, M] {
	return orm.FoundryDeleteUsing[S](destination, sourceOf(source), false, orm.SoftDeletable[M]{}, sqlowner.Queries())
}

// ForceDeleteUsing physically removes soft-delete models within the supplied visibility. Example: rows.ForceDeleteUsing(Users.Query().WithTrashed(),Users.Soft,rows.From(Archives.Query())).Match(key).
func ForceDeleteUsing[S, M any](destination orm.Query[M], soft orm.SoftDeletable[M], source SelectionSource[S]) orm.DeleteSource[S, M] {
	return orm.FoundryDeleteUsing[S](destination, sourceOf(source), true, soft, sqlowner.Queries())
}
