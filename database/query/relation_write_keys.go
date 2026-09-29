package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Internal ownership reads deliberately skip Retrieved callbacks, like the
// locked snapshots already used by ordinary model writes. Shared locks keep
// other transactions from changing an endpoint while its relation is written.
func relationWriteModel[M any](ctx context.Context, tx *database.Tx, q Query[M]) (M, error) {
	statement, err := q.ForShare().Limit(2).Compile()
	if err != nil {
		return *new(M), err
	}
	return returningOne(ctx, tx, statement, q.definition.scan)
}

type relationWriteKeys[P any] struct {
	defaults   Mutation[P]
	predicates []Predicate[P]
}

func (r ThroughRelation[M, N, P]) writeKeys(source M, target N) (relationWriteKeys[P], error) {
	left, lp, err := relationPivotKey(r.spec.source, r.spec.local, source, r.pivot, r.pivotLocal)
	if err != nil {
		return relationWriteKeys[P]{}, err
	}
	right, rp, err := relationPivotKey(r.spec.target, r.spec.foreign, target, r.pivot, r.pivotForeign)
	if err != nil {
		return relationWriteKeys[P]{}, err
	}
	defaults, err := r.pivot.withModelValue(Mutation[P]{}, r.pivotLocal.column, left)
	if err != nil {
		return relationWriteKeys[P]{}, err
	}
	defaults, err = r.pivot.withModelValue(defaults, r.pivotForeign.column, right)
	if err != nil {
		return relationWriteKeys[P]{}, err
	}
	for _, fixed := range r.pivotFixed {
		if defaults, err = r.pivot.withModelValue(defaults, fixed.column, fixed.raw); err != nil {
			return relationWriteKeys[P]{}, err
		}
	}
	return relationWriteKeys[P]{defaults: defaults, predicates: []Predicate[P]{lp, rp}}, nil
}

func relationPivotKey[M, P any](source Query[M], sourceRef fieldRef, model M, pivot Query[P], pivotRef fieldRef) (driver.Value, Predicate[P], error) {
	sourceField, ok := source.definition.modelField(sourceRef.column)
	if !ok {
		return nil, Predicate[P]{}, fault.New(fault.Invalid, "relation source key has no declared codec")
	}
	pivotField, ok := pivot.definition.modelField(pivotRef.column)
	if !ok {
		return nil, Predicate[P]{}, fault.New(fault.Invalid, "relation pivot key has no assignment codec")
	}
	raw, err := sourceField.get(model)
	if err != nil {
		return nil, Predicate[P]{}, err
	}
	if raw == nil {
		return nil, Predicate[P]{}, fault.New(fault.Missing, "relation writes require non-null endpoint keys")
	}
	// The same codec validates the comparison and automatic draft default.
	predicate := Predicate[P]{expression: comparison{operand: pivotRef, operator: equal, values: []any{raw}, bind: pivotField.decode}}
	return raw, predicate, nil
}

func (r ThroughRelation[M, N, P]) writeEndpoints(ctx context.Context, tx *database.Tx, source M, target N) (ThroughRelation[M, N, P], relationWriteKeys[P], error) {
	sp, err := modelWritePredicate(r.spec.source, source)
	if err != nil {
		return r, relationWriteKeys[P]{}, err
	}
	tp, err := modelWritePredicate(r.spec.target, target)
	if err != nil {
		return r, relationWriteKeys[P]{}, err
	}
	targetScope := r.spec.target
	r.spec.source = r.spec.source.Where(sp)
	r.spec.target = r.spec.target.Where(tp)
	source, err = relationWriteModel(ctx, tx, r.spec.source)
	if err != nil {
		return r, relationWriteKeys[P]{}, err
	}
	target, err = relationWriteModel(ctx, tx, r.spec.target)
	if err != nil {
		return r, relationWriteKeys[P]{}, err
	}
	keys, err := r.writeKeys(source, target)
	if err != nil {
		return r, keys, err
	}
	// A non-primary target key still has to identify at most one target within
	// this relation, matching through-loader cardinality rather than concealing
	// an ambiguous link behind the supplied model's primary-key predicate.
	if r.spec.foreign.column != r.spec.target.definition.primary {
		match, err := modelWriteFieldPredicate(targetScope, r.spec.foreign, target)
		if err != nil {
			return r, keys, err
		}
		if _, err = relationWriteModel(ctx, tx, targetScope.Where(match)); err != nil {
			return r, keys, err
		}
	}
	return r, keys, nil
}
