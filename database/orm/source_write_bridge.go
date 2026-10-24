package orm

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryInsertMapping binds a framework expression to its destination. Example: FoundryInsertMapping[User,Archive](mapping,sqlowner.Expressions()).
func FoundryInsertMapping[S, M any](m ormcore.ModelMapping, seal sqlowner.ExpressionSeal) InsertMapping[S, M] {
	if !seal.Valid() {
		m = ormcore.ModelMapping{Err: invalid("invalid SQL owner capability")}
	}
	return InsertMapping[S, M]{core: m}
}

// FoundryUpdateMapping binds a framework expression to its destination. Example: FoundryUpdateMapping[Archive,User](mapping,sqlowner.Expressions()).
func FoundryUpdateMapping[S, M any](m ormcore.ModelMapping, seal sqlowner.ExpressionSeal) UpdateMapping[S, M] {
	if !seal.Valid() {
		m = ormcore.ModelMapping{Err: invalid("invalid SQL owner capability")}
	}
	return UpdateMapping[S, M]{core: m}
}

// FoundrySourceKey binds a framework expression to a stored primary key. Example: FoundrySourceKey[Archive,User](mapping,sqlowner.Expressions()).
func FoundrySourceKey[S, M any](m ormcore.ModelMapping, seal sqlowner.ExpressionSeal) SourceKey[S, M] {
	if !seal.Valid() {
		m = ormcore.ModelMapping{Err: invalid("invalid SQL owner capability")}
	}
	return SourceKey[S, M]{core: m}
}

// FoundryInsertFrom creates a source insertion plan. Example: FoundryInsertFrom[User](Archives.Query(),source,sqlowner.Queries()).
func FoundryInsertFrom[S, M any](q Query[M], source ormcore.Source, seal sqlowner.QuerySeal) InsertSelect[S, M] {
	return InsertSelect[S, M]{plan: newSourceWrite(q.state, source, reflect.TypeFor[S](), querycore.InsertModel, seal)}
}

// FoundryUpdateFrom creates a source update plan. Example: FoundryUpdateFrom[Archive](Users.Query(),source,sqlowner.Queries()).
func FoundryUpdateFrom[S, M any](q Query[M], source ormcore.Source, seal sqlowner.QuerySeal) UpdateSource[S, M] {
	return UpdateSource[S, M]{plan: newSourceWrite(q.state, source, reflect.TypeFor[S](), querycore.UpdateModel, seal)}
}

// FoundryDeleteUsing creates a source removal plan with explicit visibility. Example: FoundryDeleteUsing[Archive](Users.Query(),source,false,SoftDeletable[User]{},sqlowner.Queries()).
func FoundryDeleteUsing[S, M any](q Query[M], source ormcore.Source, force bool, soft SoftDeletable[M], seal sqlowner.QuerySeal) DeleteSource[S, M] {
	kind := querycore.DeleteModel
	if force {
		if err := checkSoftDelete(soft.capability, q.state.table); err != nil {
			return DeleteSource[S, M]{plan: sourceWritePlan{err: err}}
		}
		kind = querycore.ForceDeleteModel
	}
	return DeleteSource[S, M]{plan: newSourceWrite(q.state, source, reflect.TypeFor[S](), kind, seal)}
}
func newSourceWrite(q queryState, source ormcore.Source, scope reflect.Type, kind querycore.MutationKind, seal sqlowner.QuerySeal) sourceWritePlan {
	if !seal.Valid() {
		return sourceWritePlan{err: invalid("invalid SQL owner capability")}
	}
	if source.Scope != scope {
		return sourceWritePlan{err: invalid("source write has a different source scope")}
	}
	return sourceWritePlan{query: q, source: source, kind: kind}
}
