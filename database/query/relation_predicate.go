package query

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ExistenceRelation is a declared relationship that can filter its parent by
// matching rows. Computed aggregate slots are not existence relationships.
type ExistenceRelation[M any] interface {
	Relation[M]
	Exists() Predicate[M]
}

// WhereHas filters parent models by matching related rows. Use the descriptor's
// Where/WherePivot methods to scope matches. It does not load the relationship.
func (q Query[M]) WhereHas(r ExistenceRelation[M]) Query[M] {
	return q.Where(relationExists(r))
}

// WhereDoesntHave filters parent models with no matching related row. Nullable
// relationship keys follow SQL equality; absent keys do not create a match.
func (q Query[M]) WhereDoesntHave(r ExistenceRelation[M]) Query[M] {
	return q.Where(relationExists(r).Not())
}

func relationExists[M any](r ExistenceRelation[M]) Predicate[M] {
	if nilDescriptor(r) {
		return Predicate[M]{}
	}
	return r.Exists()
}

// Exists produces a parent-owned predicate without hydrating a related model.
// It asks whether a match exists, including when several rows match a One slot.
func (r OneRelation[M, N]) Exists() Predicate[M] { return r.spec.exists() }

// Exists produces a parent-owned predicate using this relation's target filters.
// Ordering and eager-loading clauses do not affect existence or perform I/O.
func (r ManyRelation[M, N]) Exists() Predicate[M] { return r.spec.exists() }

// Exists filters parents through matching target/pivot rows. It preserves both
// filter scopes and uses framework-owned aliases, including nested self-links.
func (r ThroughRelation[M, N, P]) Exists() Predicate[M] {
	r.spec.target = r.spec.target.existenceScope()
	r.pivot = r.pivot.existenceScope()
	r.orders = nil
	if err := r.validateMetadata(r.spec.source.queryCore.Table, 0, DefaultRelationLimits()); err != nil {
		return existsPredicate[M](subquery{Err: err})
	}
	names, err := namesInSelects(r.spec.target.modelSelect(), r.pivot.modelSelect())
	if err != nil {
		return existsPredicate[M](subquery{Err: err})
	}
	names.Used[r.spec.source.queryCore.Table] = true
	target, pivot := names.Allocate("foundry_related"), names.Allocate("foundry_link")
	node := r.throughSelectAt(target, pivot)
	node.Selections, node.Orders = nil, nil
	node.Predicates = append(node.Predicates, binaryComparison{
		Left: fieldRef{Table: pivot, Column: r.pivotLocal.Column}, Right: r.spec.local, Operator: equal,
	})
	return r.spec.existencePredicate(node)
}

// Only filters contribute to existence. Descriptor ordering/loading remains
// available when the same descriptor is reused in an ordinary With operation.
func (q Query[M]) existenceScope() Query[M] {
	q.queryCore.Orders, q.relations, q.relationLimits = nil, nil, nil
	return q
}

func (s relationSpec[M, N]) exists() Predicate[M] {
	s.target = s.target.existenceScope()
	if err := s.validate(s.source.queryCore.Table, 0, DefaultRelationLimits()); err != nil {
		return existsPredicate[M](subquery{Err: err})
	}
	if s.hop == nil && s.morph == nil {
		return existsPredicate[M](querycore.DirectRelationExists(s.target.modelSelect(), s.local, s.foreign, s.source.definition.modelDefinition.Columns))
	}
	node, alias, err := querycore.RelationExistsSelect(s.target.modelSelect(), s.source.queryCore.Table)
	if err != nil {
		return existsPredicate[M](subquery{Err: err})
	}
	if s.hop != nil {
		// Through an intermediate: target -> intermediate.second, and the
		// intermediate's first key correlates with the outer source key.
		node = s.hop.join(node, alias, s.foreign)
		node.Predicates = append(node.Predicates, binaryComparison{
			Left: fieldRef{Table: hopAlias, Column: s.hop.first.Column}, Right: s.local, Operator: equal,
		})
		return s.existencePredicate(node)
	}
	node.Predicates = append(node.Predicates, binaryComparison{
		Left: fieldRef{Table: alias, Column: s.foreign.Column}, Right: s.local, Operator: equal,
	})
	if s.morph != nil {
		// The outer row must also store this target type's morph name.
		node.Predicates = append(node.Predicates, comparison{Operand: fieldRef{Table: s.source.queryCore.Table, Column: s.morph.kind.Column}, Operator: equal,
			Values: []any{string(s.morph.name)}, Bind: func(v any) (driver.Value, error) { return v, nil }})
	}
	return s.existencePredicate(node)
}

func (s relationSpec[M, N]) existencePredicate(node selectNode) Predicate[M] {
	return existsPredicate[M](querycore.RelationExistence(node, s.source.queryCore.Table, s.source.definition.modelDefinition.Columns))
}

func existsPredicate[M any](q subquery) Predicate[M] {
	return Predicate[M]{expression: subqueryPredicate{Query: q}}
}
