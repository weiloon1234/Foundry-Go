package query

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
	if err := r.validateMetadata(r.spec.source.table, 0, DefaultRelationLimits()); err != nil {
		return existsPredicate[M](subquery{err: err})
	}
	names, err := namesInSelects(r.spec.target.modelSelect(), r.pivot.modelSelect())
	if err != nil {
		return existsPredicate[M](subquery{err: err})
	}
	names.used[r.spec.source.table] = true
	target, pivot := names.allocate("foundry_related"), names.allocate("foundry_link")
	node := r.throughSelectAt(target, pivot)
	node.selections, node.orders = nil, nil
	node.predicates = append(node.predicates, binaryComparison{
		left: fieldRef{pivot, r.pivotLocal.column}, right: r.spec.local, operator: equal,
	})
	return r.spec.existencePredicate(node)
}

// Only filters contribute to existence. Descriptor ordering/loading remains
// available when the same descriptor is reused in an ordinary With operation.
func (q Query[M]) existenceScope() Query[M] {
	q.orders, q.relations, q.relationLimits = nil, nil, nil
	return q
}
func (s relationSpec[M, N]) exists() Predicate[M] {
	s.target = s.target.existenceScope()
	if err := s.validate(s.source.table, 0, DefaultRelationLimits()); err != nil {
		return existsPredicate[M](subquery{err: err})
	}
	node := s.target.modelSelect()
	node.selections = nil
	names, err := namesInSelects(node)
	if err != nil {
		return existsPredicate[M](subquery{err: err})
	}
	names.used[s.source.table] = true
	alias := names.allocate("foundry_related")
	node.source.alias = alias
	node.predicates = nil
	for _, predicate := range s.target.effectivePredicates() {
		node.predicates = append(node.predicates, requalify(predicate, alias))
	}
	node.predicates = append(node.predicates, binaryComparison{
		left: fieldRef{alias, s.foreign.column}, right: s.local, operator: equal,
	})
	return s.existencePredicate(node)
}
func (s relationSpec[M, N]) existencePredicate(node selectNode) Predicate[M] {
	return existsPredicate[M](subquery{node: node, correlation: &scopeRequirement{
		sources: map[string][]Column{s.source.table: s.source.definition.columns},
	}})
}
func existsPredicate[M any](q subquery) Predicate[M] {
	return Predicate[M]{expression: subqueryPredicate{query: q}}
}
