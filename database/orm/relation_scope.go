package orm

// Where constrains related rows. Example: OrderRelations.Buyer.Where(Users.Age.Gt(18)).
func (r BelongsTo[M, N]) Where(ps ...Predicate[N]) BelongsTo[M, N] {
	r.plan = relationWhere(r.plan, ps)
	return r
}

// Where constrains related rows. Example: UserRelations.Profile.Where(Profiles.Active.Eq(true)).
func (r HasOne[M, N]) Where(ps ...Predicate[N]) HasOne[M, N] {
	r.plan = relationWhere(r.plan, ps)
	return r
}

// Where constrains related rows. Example: UserRelations.Orders.Where(Orders.TotalCents.Gt(100)).
func (r HasMany[M, N]) Where(ps ...Predicate[N]) HasMany[M, N] {
	r.plan = relationWhere(r.plan, ps)
	return r
}

// OrderBy orders related rows with a primary-key tie-breaker. Example: OrderRelations.Buyer.OrderBy(Users.Email.Asc()).
func (r BelongsTo[M, N]) OrderBy(os ...Order[N]) BelongsTo[M, N] {
	r.plan = relationOrder(r.plan, os)
	return r
}

// OrderBy orders related rows without hiding cardinality errors. Example: UserRelations.Profile.OrderBy(Profiles.ID.Asc()).
func (r HasOne[M, N]) OrderBy(os ...Order[N]) HasOne[M, N] {
	r.plan = relationOrder(r.plan, os)
	return r
}

// OrderBy orders related collections. Example: UserRelations.Orders.OrderBy(Orders.TotalCents.Desc()).
func (r HasMany[M, N]) OrderBy(os ...Order[N]) HasMany[M, N] {
	r.plan = relationOrder(r.plan, os)
	return r
}

// With batches nested branches over all fetched targets. Example: OrderRelations.Buyer.With(UserRelations.Orders).
func (r BelongsTo[M, N]) With(children ...Relation[N]) BelongsTo[M, N] {
	r.plan = relationWith(r.plan, children)
	return r
}

// With batches nested branches over all fetched targets. Example: UserRelations.Profile.With(ProfileRelations.Photo).
func (r HasOne[M, N]) With(children ...Relation[N]) HasOne[M, N] {
	r.plan = relationWith(r.plan, children)
	return r
}

// With batches nested branches over all fetched targets. Example: UserRelations.Orders.With(OrderRelations.Buyer).
func (r HasMany[M, N]) With(children ...Relation[N]) HasMany[M, N] {
	r.plan = relationWith(r.plan, children)
	return r
}

func copyRelationPlan(p *relationPlan) *relationPlan {
	if p == nil {
		return &relationPlan{err: invalid("nil relation descriptor")}
	}
	copy := *p
	return &copy
}
func relationWhere[N any](p *relationPlan, ps []Predicate[N]) *relationPlan {
	p = copyRelationPlan(p)
	p.query = where(p.query, ps)
	return p
}
func relationOrder[N any](p *relationPlan, os []Order[N]) *relationPlan {
	p = copyRelationPlan(p)
	p.query = orderBy(p.query, os)
	return p
}
func relationWith[N any](p *relationPlan, children []Relation[N]) *relationPlan {
	p = copyRelationPlan(p)
	p.query = withRelations(p.query, children)
	return p
}
