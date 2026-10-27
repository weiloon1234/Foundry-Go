package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
)

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

// WithTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithTrashed().
func (r BelongsTo[M, N]) WithTrashed() BelongsTo[M, N] {
	r.plan = relationVisibility(r.plan, querycore.AllRecords, false)
	return r
}

// OnlyTrashed sets target soft-delete visibility. Example: UserRelations.Orders.OnlyTrashed().
func (r BelongsTo[M, N]) OnlyTrashed() BelongsTo[M, N] {
	r.plan = relationVisibility(r.plan, querycore.TrashedRecords, false)
	return r
}

// WithoutTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithoutTrashed().
func (r BelongsTo[M, N]) WithoutTrashed() BelongsTo[M, N] {
	r.plan = relationVisibility(r.plan, querycore.ActiveRecords, false)
	return r
}

// WithTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithTrashed().
func (r HasOne[M, N]) WithTrashed() HasOne[M, N] {
	r.plan = relationVisibility(r.plan, querycore.AllRecords, false)
	return r
}

// OnlyTrashed sets target soft-delete visibility. Example: UserRelations.Orders.OnlyTrashed().
func (r HasOne[M, N]) OnlyTrashed() HasOne[M, N] {
	r.plan = relationVisibility(r.plan, querycore.TrashedRecords, false)
	return r
}

// WithoutTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithoutTrashed().
func (r HasOne[M, N]) WithoutTrashed() HasOne[M, N] {
	r.plan = relationVisibility(r.plan, querycore.ActiveRecords, false)
	return r
}

// WithTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithTrashed().
func (r HasMany[M, N]) WithTrashed() HasMany[M, N] {
	r.plan = relationVisibility(r.plan, querycore.AllRecords, false)
	return r
}

// OnlyTrashed sets target soft-delete visibility. Example: UserRelations.Orders.OnlyTrashed().
func (r HasMany[M, N]) OnlyTrashed() HasMany[M, N] {
	r.plan = relationVisibility(r.plan, querycore.TrashedRecords, false)
	return r
}

// WithoutTrashed sets target soft-delete visibility. Example: UserRelations.Orders.WithoutTrashed().
func (r HasMany[M, N]) WithoutTrashed() HasMany[M, N] {
	r.plan = relationVisibility(r.plan, querycore.ActiveRecords, false)
	return r
}

// OfMany chooses the first target per parent in these orders, then primary key. Example: UserRelations.LatestOrder.OfMany(Orders.TotalCents.Desc()).
func (r HasOne[M, N]) OfMany(orders ...Order[N]) HasOne[M, N] {
	r.plan = ofMany(relationOrder(r.plan, orders), 1)
	return r
}

// LatestOfMany chooses the highest target primary key. Example: UserRelations.LatestOrder.LatestOfMany().
func (r HasOne[M, N]) LatestOfMany() HasOne[M, N] { r.plan = ofMany(r.plan, 2); return r }

// OldestOfMany chooses the lowest target primary key. Example: UserRelations.LatestOrder.OldestOfMany().
func (r HasOne[M, N]) OldestOfMany() HasOne[M, N] { r.plan = ofMany(r.plan, 3); return r }

// OfMany chooses one target per local key using these orders and the primary key. Example: OrderRelations.Buyer.OfMany(Users.ID.Asc()).
func (r BelongsTo[M, N]) OfMany(orders ...Order[N]) BelongsTo[M, N] {
	r.plan = ofMany(relationOrder(r.plan, orders), 1)
	return r
}

// LatestOfMany chooses the highest target primary key. Example: OrderRelations.Buyer.LatestOfMany().
func (r BelongsTo[M, N]) LatestOfMany() BelongsTo[M, N] { r.plan = ofMany(r.plan, 2); return r }

// OldestOfMany chooses the lowest target primary key. Example: OrderRelations.Buyer.OldestOfMany().
func (r BelongsTo[M, N]) OldestOfMany() BelongsTo[M, N] { r.plan = ofMany(r.plan, 3); return r }

// Expression filters and EXISTS share target-owner and scope validation.
func relationExpressionPredicate[N any](plan *relationPlan, p ormcore.Predicate) Predicate[N] {
	source := ormcore.Source{Scope: reflect.TypeFor[N](), Owners: map[string]any{plan.target.model.Table: plan.target}}
	return Predicate[N]{owner: plan.target, expression: querycore.RetainPredicate(p.Node), err: safeerror.Join("operation failed", p.Err, ormcore.CheckReferenceSet(source, p.Refs, p.ReferenceFree))}
}
