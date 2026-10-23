package ormcore

import (
	"errors"
	"maps"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// JoinSources retains ownership and declaration checks around the shared SQL AST.
func JoinSources(left, right Source, on querycore.Expression, leftRefs, rightRefs []Reference, onError error, kind querycore.JoinKind, scope reflect.Type) Source {
	err := onError
	if kind != querycore.CrossJoin {
		err = errors.Join(err, CheckReferences(left, leftRefs), CheckReferences(right, rightRefs))
	}
	if len(right.Node.Joins) > 0 {
		err = errors.Join(err, Invalid("right join input must be an aliased source"))
	}
	node := querycore.JoinNodes(left.Node, right.Node, left.Aliases, right.Aliases, errors.Join(left.Err, err), right.Err, on, kind)
	owners := maps.Clone(left.Owners)
	if owners == nil {
		owners = map[string]any{}
	}
	maps.Copy(owners, right.Owners)
	return Source{Scope: scope, Node: node.Node, Aliases: node.Aliases, Owners: owners, Err: node.Err, Checks: AppendChecks(left.Checks, right.Checks...)}
}

// Rescope validates the declaration and side before retaining a model reference.
func Rescope(scope Scope, owners map[string]any, err error) Scope {
	scope.Err = errors.Join(scope.Err, err)
	if scope.Owner == nil || owners[scope.Table] != scope.Owner {
		scope.Err = errors.Join(scope.Err, Invalid("scope is not on this join side"))
	}
	return scope
}
