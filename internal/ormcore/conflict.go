package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ConflictPolicy carries immutable SQL policy data and concrete declaration ownership.
type ConflictPolicy struct {
	Policy querycore.Conflict
	Owner  any
	Err    error
}

// ConflictAssignment retains the declared destination beside its calculation.
type ConflictAssignment struct {
	Update querycore.ConflictUpdate
	Owner  any
	Err    error
}

func ConflictVisibility(source Source, scope reflect.Type) VisibleScope {
	out := VisibleScope{Scope: scope, Checks: AppendChecks(source.Checks), Required: querycore.ScopeRequirement{Err: source.Err}}
	owner := source.Owners[source.Node.Source.Name()]
	if owner == nil || len(source.Node.Source.Columns) == 0 {
		out.Required.Err = safeerror.Join("operation failed", out.Required.Err, Invalid("conflict rows require complete model metadata"))
	}
	out.Owners = map[string]any{querycore.ConflictStoredTable: owner, querycore.ConflictProposedTable: owner}
	out.Required.Sources = map[string][]querycore.Column{
		querycore.ConflictStoredTable:   slices.Clone(source.Node.Source.Columns),
		querycore.ConflictProposedTable: slices.Clone(source.Node.Source.Columns),
	}
	return out
}

func ConflictScope(source VisibleScope, table string) Scope {
	return Scope{Reference: Reference{Scope: source.Scope, Table: table, Owner: source.Owners[table]}, Err: source.Required.Err}
}

func ConflictAnchor(source VisibleScope) Source {
	return Source{Scope: source.Scope, Owners: source.Owners, Checks: AppendChecks(source.Checks), Err: source.Required.Err}
}

func conflictReferences(owner any, scope reflect.Type, refs []Reference, free, rows bool) (any, error) {
	owners := map[string]any{}
	if rows {
		owners[querycore.ConflictStoredTable] = owner
		owners[querycore.ConflictProposedTable] = owner
	} else {
		for _, ref := range refs {
			if owner == nil {
				owner = ref.Owner
			}
			owners[ref.Table] = owner
		}
	}
	if owner == nil {
		return nil, Invalid("conflict expression requires a declared destination")
	}
	// A reference-free constant still requires the policy's established owner.
	if len(owners) == 0 {
		owners[""] = owner
	}
	return owner, CheckReferenceSet(Source{Scope: scope, Owners: owners}, refs, free)
}

func ConflictKeys(scope reflect.Type, keys []Expression) ConflictPolicy {
	p := ConflictPolicy{}
	for _, key := range keys {
		owner, err := conflictReferences(p.Owner, scope, key.Refs, key.ReferenceFree, false)
		p.Owner = owner
		p.Err = safeerror.Join("operation failed", p.Err, key.Err, err)
		p.Policy.Keys = append(p.Policy.Keys, key.Node)
	}
	return p
}

func ConflictPredicates(p ConflictPolicy, scope reflect.Type, predicates []Predicate, rows bool) ConflictPolicy {
	nodes := slices.Clone(p.Policy.TargetCondition)
	if rows {
		nodes = slices.Clone(p.Policy.RowCondition)
	}
	for _, predicate := range predicates {
		// Row predicates may establish an owner on an unbound named policy.
		if p.Owner == nil && rows && len(predicate.Refs) > 0 {
			p.Owner = predicate.Refs[0].Owner
		}
		owner, err := conflictReferences(p.Owner, scope, predicate.Refs, predicate.ReferenceFree, rows)
		p.Owner = owner
		p.Err = safeerror.Join("operation failed", p.Err, predicate.Err, err)
		nodes = append(nodes, predicate.Node)
	}
	if rows {
		p.Policy.RowCondition = nodes
	} else {
		p.Policy.TargetCondition = nodes
	}
	return p
}

func ConflictCalculation(destination Reference, field querycore.FieldRef, err error, scope reflect.Type, value Expression) ConflictAssignment {
	_, referenceErr := conflictReferences(destination.Owner, scope, value.Refs, value.ReferenceFree, true)
	return ConflictAssignment{Owner: destination.Owner, Err: safeerror.Join("operation failed", err, value.Err, referenceErr), Update: querycore.ConflictUpdate{Field: field, Value: value.Node}}
}
