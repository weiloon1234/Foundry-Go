package querycore

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type SoftDeleteScope uint8

type SoftDeleteColumn struct{ Name string }

const (
	ActiveRecords SoftDeleteScope = iota
	AllRecords
	TrashedRecords
)

func (q QueryCore) HasSoftDeletes() bool { return q.Model != nil && q.Model.SoftDelete != nil }

func (q QueryCore) ValidateSoftDeleteScope() error {
	if q.SoftDeleteScope > TrashedRecords || (q.SoftDeleteScope != ActiveRecords && !q.HasSoftDeletes()) {
		return safeerror.NewFault(fault.Invalid, "trashed visibility requires a soft-delete model declaration")
	}
	return nil
}

// effectivePredicates is the sole source of automatic model visibility. Keep
// explicit filters separate so replacing the implicit scope never removes one.
// Model global scopes join here too, so every read, write, relation and
// subquery path that honors soft deletion honors them identically.
func (q QueryCore) EffectivePredicates() []Expression {
	scopes := q.ScopePredicates()
	if (!q.HasSoftDeletes() || q.SoftDeleteScope == AllRecords) && len(scopes) == 0 {
		return q.Predicates
	}
	result := append(slices.Clone(q.Predicates), scopes...)
	if !q.HasSoftDeletes() || q.SoftDeleteScope == AllRecords {
		return result
	}
	op := IsNull
	if q.SoftDeleteScope == TrashedRecords {
		op = IsNotNull
	}
	return append(result, q.DeletionPredicate(op))
}

func (q QueryCore) DeletionPredicate(op Operator) Expression {
	return Comparison{Operand: FieldRef{q.Table, q.Model.SoftDelete.Name}, Operator: op}
}
