package query

import "context"

// WithoutGlobalScope removes the target's named scopes for this relation only.
func (r OneRelation[M, N]) WithoutGlobalScope(scopes ...GlobalScope[N]) OneRelation[M, N] {
	r.spec.target = r.spec.target.WithoutGlobalScope(scopes...)
	return r
}

// WithoutGlobalScopes removes every target scope for this relation only.
func (r OneRelation[M, N]) WithoutGlobalScopes() OneRelation[M, N] {
	r.spec.target = r.spec.target.WithoutGlobalScopes()
	return r
}

// WithoutGlobalScope removes the target's named scopes for this relation only.
func (r ManyRelation[M, N]) WithoutGlobalScope(scopes ...GlobalScope[N]) ManyRelation[M, N] {
	r.spec.target = r.spec.target.WithoutGlobalScope(scopes...)
	return r
}

// WithoutGlobalScopes removes every target scope for this relation only.
func (r ManyRelation[M, N]) WithoutGlobalScopes() ManyRelation[M, N] {
	r.spec.target = r.spec.target.WithoutGlobalScopes()
	return r
}

// WithoutGlobalScope removes the target's named scopes for this relation only.
func (r ThroughRelation[M, N, P]) WithoutGlobalScope(scopes ...GlobalScope[N]) ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.WithoutGlobalScope(scopes...)
	return r
}

// WithoutGlobalScopes removes every target scope for this relation only.
func (r ThroughRelation[M, N, P]) WithoutGlobalScopes() ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.WithoutGlobalScopes()
	return r
}

// WithoutPivotGlobalScope removes the pivot's named scopes for this relation.
func (r ThroughRelation[M, N, P]) WithoutPivotGlobalScope(scopes ...GlobalScope[P]) ThroughRelation[M, N, P] {
	r.pivot = r.pivot.WithoutGlobalScope(scopes...)
	return r
}

// WithoutPivotGlobalScopes removes every pivot scope for this relation.
func (r ThroughRelation[M, N, P]) WithoutPivotGlobalScopes() ThroughRelation[M, N, P] {
	r.pivot = r.pivot.WithoutGlobalScopes()
	return r
}

// WithoutGlobalScope removes named scopes while retaining the row lock.
func (q LockedQuery[M]) WithoutGlobalScope(scopes ...GlobalScope[M]) LockedQuery[M] {
	q.query = q.query.WithoutGlobalScope(scopes...)
	return q
}

// WithoutGlobalScopes removes every scope while retaining the row lock.
func (q LockedQuery[M]) WithoutGlobalScopes() LockedQuery[M] {
	q.query = q.query.WithoutGlobalScopes()
	return q
}

// WithScopeContext resolves context scopes for Compile and plan inspection.
func (q LockedQuery[M]) WithScopeContext(ctx context.Context) LockedQuery[M] {
	q.query = q.query.WithScopeContext(ctx)
	return q
}
