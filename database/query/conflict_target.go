package query

// OnConflictKeys infers a unique index from model-owned row keys. Use generated
// field Group methods and computed row expressions such as Lower(field).Group().
// Physical index expressions and operator types must match the migration.
// An empty key list supports DoNothing; DoUpdate requires an explicit target.
func OnConflictKeys[M any](keys ...Group[M]) Conflict[M] {
	return Conflict[M]{keys: keyExpressions(keys)}
}

// TargetWhere declares a partial unique index predicate. It is separate from
// Where/WhereRows, which control whether the conflicting row is updated.
// Use fixed migration values: target constants are rendered into inspected SQL
// for reliable index inference with generic prepared plans. Insert values,
// update calculations and update conditions continue to use bound parameters.
func (c Conflict[M]) TargetWhere(predicates ...Predicate[M]) Conflict[M] {
	c.targetCondition = appendPredicates(c.targetCondition, predicates)
	return c
}
