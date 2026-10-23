package orm

// scopeRelation preserves the exact descriptor kind through package functions.
type scopeRelation[M, N, R any] interface {
	ExistenceRelation[M, N]
	withScopePlan(*relationPlan) R
}

func (r BelongsTo[M, N]) withScopePlan(p *relationPlan) BelongsTo[M, N] { r.plan = p; return r }
func (r HasOne[M, N]) withScopePlan(p *relationPlan) HasOne[M, N]       { r.plan = p; return r }
func (r HasMany[M, N]) withScopePlan(p *relationPlan) HasMany[M, N]     { r.plan = p; return r }
func (r ThroughRelation[M, N, P]) withScopePlan(p *relationPlan) ThroughRelation[M, N, P] {
	r.plan = p
	return r
}

// WithoutRelationGlobalScope removes named target defaults without changing the
// relation kind or its pivot defaults. Example: WithoutRelationGlobalScope(UserRelations.Orders, orderTenantScope).Where(Orders.Active.Eq(true)).
func WithoutRelationGlobalScope[M, N any, R scopeRelation[M, N, R]](r R, scopes ...GlobalScope[N]) R {
	p := copyRelationPlan(r.relationPlan(nil))
	p.query = withoutGlobalScope(p.query, scopes)
	return r.withScopePlan(p)
}

// WithoutRelationGlobalScopes removes all target defaults from this descriptor.
// Example: WithoutRelationGlobalScopes(UserRelations.Orders).
func WithoutRelationGlobalScopes[M, N any, R scopeRelation[M, N, R]](r R) R {
	p := copyRelationPlan(r.relationPlan(nil))
	p.query.ast.AllScopesOff = true
	return r.withScopePlan(p)
}

// WithoutPivotGlobalScope removes named pivot defaults only.
// Example: WithoutPivotGlobalScope(UserRelations.Groups, membershipTenantScope).
func WithoutPivotGlobalScope[M, N, P any](r ThroughRelation[M, N, P], scopes ...GlobalScope[P]) ThroughRelation[M, N, P] {
	p := copyJoined(r.plan)
	if p.join != nil {
		p.join.pivot = withoutGlobalScope(p.join.pivot, scopes)
	}
	r.plan = p
	return r
}

// WithoutPivotGlobalScopes removes every pivot default only.
// Example: WithoutPivotGlobalScopes(UserRelations.Groups).
func WithoutPivotGlobalScopes[M, N, P any](r ThroughRelation[M, N, P]) ThroughRelation[M, N, P] {
	p := copyJoined(r.plan)
	if p.join != nil {
		p.join.pivot.ast.AllScopesOff = true
	}
	r.plan = p
	return r
}
