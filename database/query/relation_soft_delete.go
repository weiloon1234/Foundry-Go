package query

// WithTrashed includes soft-deleted targets without changing the parent scope.
func (r OneRelation[M, N]) WithTrashed() OneRelation[M, N] {
	r.spec.target = r.spec.target.WithTrashed()
	return r
}

// OnlyTrashed selects only soft-deleted targets.
func (r OneRelation[M, N]) OnlyTrashed() OneRelation[M, N] {
	r.spec.target = r.spec.target.OnlyTrashed()
	return r
}

// WithoutTrashed restores the target's default active-record scope.
func (r OneRelation[M, N]) WithoutTrashed() OneRelation[M, N] {
	r.spec.target = r.spec.target.WithoutTrashed()
	return r
}

// WithTrashed includes soft-deleted targets without changing the parent scope.
func (r ManyRelation[M, N]) WithTrashed() ManyRelation[M, N] {
	r.spec.target = r.spec.target.WithTrashed()
	return r
}

// OnlyTrashed selects only soft-deleted targets.
func (r ManyRelation[M, N]) OnlyTrashed() ManyRelation[M, N] {
	r.spec.target = r.spec.target.OnlyTrashed()
	return r
}

// WithoutTrashed restores the target's default active-record scope.
func (r ManyRelation[M, N]) WithoutTrashed() ManyRelation[M, N] {
	r.spec.target = r.spec.target.WithoutTrashed()
	return r
}

// WithTrashed includes soft-deleted targets; pivot visibility remains independent.
func (r ThroughRelation[M, N, P]) WithTrashed() ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.WithTrashed()
	return r
}

// OnlyTrashed selects soft-deleted targets; pivot visibility remains independent.
func (r ThroughRelation[M, N, P]) OnlyTrashed() ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.OnlyTrashed()
	return r
}

// WithoutTrashed restores the target's default active-record scope.
func (r ThroughRelation[M, N, P]) WithoutTrashed() ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.WithoutTrashed()
	return r
}

// WithTrashedPivot includes soft-deleted pivots without changing target visibility.
func (r ThroughRelation[M, N, P]) WithTrashedPivot() ThroughRelation[M, N, P] {
	r.pivot = r.pivot.WithTrashed()
	return r
}

// OnlyTrashedPivot selects soft-deleted pivots without changing target visibility.
func (r ThroughRelation[M, N, P]) OnlyTrashedPivot() ThroughRelation[M, N, P] {
	r.pivot = r.pivot.OnlyTrashed()
	return r
}

// WithoutTrashedPivot restores the pivot's default active-record scope.
func (r ThroughRelation[M, N, P]) WithoutTrashedPivot() ThroughRelation[M, N, P] {
	r.pivot = r.pivot.WithoutTrashed()
	return r
}
