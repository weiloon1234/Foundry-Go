package ormcore

import (
	"errors"
	"maps"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// JoinSources retains ownership and declaration checks around the shared SQL AST.
func JoinSources(left, right Source, on JoinCondition, kind querycore.JoinKind, scope reflect.Type) Source {
	// Both the SQL and its boundary metadata come from the same rowSource call.
	// Promoted marker methods cannot authorize a different embedded source.
	if err := errors.Join(left.Err, right.Err, validateJoinSource(left, true), validateJoinSource(right, false)); err != nil {
		return Source{Scope: scope, Err: err}
	}
	err := on.Err
	if kind != querycore.CrossJoin {
		err = errors.Join(err, CheckReferenceSet(left, on.Left, on.LeftFree), CheckReferenceSet(right, on.Right, on.RightFree))
	}
	node := querycore.JoinNodes(left.Node, right.Node, left.Aliases, right.Aliases, errors.Join(left.Err, err), right.Err, on.Node, kind)
	owners := maps.Clone(left.Owners)
	if owners == nil {
		owners = map[string]any{}
	}
	maps.Copy(owners, right.Owners)
	return Source{Scope: scope, Node: node.Node, Aliases: node.Aliases, Owners: owners, Err: node.Err, Checks: AppendChecks(left.Checks, right.Checks...)}
}

// validateJoinSource admits only the boundary shape produced by As and joins.
func validateJoinSource(source Source, allowChain bool) error {
	node := source.Node
	if source.Scope == nil || len(source.Aliases) != len(node.Joins)+1 || len(source.Owners) != len(source.Aliases) || (!allowChain && len(node.Joins) != 0) {
		return Invalid("join requires an aliased source boundary")
	}
	// Removing only the allowed boundary fields makes new SELECT clauses fail
	// closed too, until they have an explicit join-boundary contract.
	node.Source = querycore.TableSource{}
	node.Joins = nil
	if !reflect.ValueOf(node).IsZero() {
		return Invalid("join source clauses require an alias boundary")
	}
	check := func(table querycore.TableSource) bool {
		return table.Alias != "" && source.Aliases[table.Alias] != nil && source.Owners[table.Alias] != nil
	}
	if !check(source.Node.Source) {
		return Invalid("join requires an aliased source boundary")
	}
	if len(source.Node.Joins) == 0 && source.Aliases[source.Node.Source.Alias] != source.Scope {
		return Invalid("join alias belongs to a different scope")
	}
	for _, join := range source.Node.Joins {
		if !check(join.Source) {
			return Invalid("join requires an aliased source boundary")
		}
	}
	return nil
}

// Rescope validates the declaration and side before retaining a model reference.
func Rescope(scope Scope, owners map[string]any, err error) Scope {
	scope.Err = errors.Join(scope.Err, err)
	if scope.Owner == nil || owners[scope.Table] != scope.Owner {
		scope.Err = errors.Join(scope.Err, Invalid("scope is not on this join side"))
	}
	return scope
}
