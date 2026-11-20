package orm

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ConflictColumn is a model-owned unique-index column. Example: orm.ConflictKey(Users.Email).
type ConflictColumn[M any] struct {
	_      [0]*M
	column *columnCore
}

// ConflictKey selects a declared column for unique-index inference.
// Example: orm.OnConflict(orm.ConflictKey(Users.Email)).DoNothing().
func ConflictKey[M, V, I, P any, K ColumnKind, R ColumnRole](field ColumnHandle[M, V, I, P, K, R]) ConflictColumn[M] {
	return ConflictColumn[M]{column: columnCoreOf(field.stored)}
}

// ConflictUpdate is a model-owned assignment in DO UPDATE. Example: orm.ConflictIncoming(Users.Email).
type ConflictUpdate[M any] struct {
	_     [0]*M
	state conflictUpdate
}

type conflictUpdate struct {
	update querycore.ConflictUpdate
	owner  *tableCore
	err    error
}

// String redacts literal inputs. Example: fmt.Sprint(orm.ConflictSet(Users.Email,"ada@example.test")).
func (ConflictUpdate[M]) String() string { return "orm conflict update" }

// GoString redacts literal inputs. Example: fmt.Sprintf("%#v",update).
func (ConflictUpdate[M]) GoString() string { return "orm conflict update" }

// ConflictSet captures the column's write input, including distinct mutator inputs.
// Encrypted fields have no conflict assignment capability.
// Example: orm.ConflictSet(Members.Email, EmailInput{Address:"ada@example.test"}).
func ConflictSet[M, I, V, P any, K ColumnKind, R ConflictSetRole](field ColumnHandle[M, V, I, P, K, R], input I) ConflictUpdate[M] {
	a := Set(field, input)
	return ConflictUpdate[M]{state: conflictUpdate{owner: a.owner, err: a.err, update: querycore.ConflictUpdate{Field: a.core.Field, Literal: &a.core.AssignmentValue}}}
}

// ConflictNull clears a nullable field without invoking its mutator.
// Example: orm.ConflictNull(Users.Nickname).
func ConflictNull[M, V, I, P any, K NullableComparableKind, R ConflictSetRole](field ColumnHandle[M, V, I, P, K, R]) ConflictUpdate[M] {
	c := columnCoreOf(field.stored)
	if c == nil || c.owner == nil || c.nullableProjection == nil {
		return ConflictUpdate[M]{state: conflictUpdate{err: invalid("uninitialized conflict field")}}
	}
	if err := declarationCoreError(c.owner); err != nil {
		return ConflictUpdate[M]{state: conflictUpdate{err: invalid("uninitialized conflict field")}}
	}
	return ConflictUpdate[M]{state: conflictUpdate{owner: c.owner, update: querycore.ConflictUpdate{Field: querycore.FieldRef{Table: c.owner.model.Table, Column: c.metadata.Name}, Null: true}}}
}

// ConflictIncoming copies the destination's own EXCLUDED value, already transformed.
// Omitted insert values copy the database default or NULL. Encrypted fields are excluded.
// Example: orm.ConflictIncoming(Users.Email).
func ConflictIncoming[M, V, I, P any, K ColumnKind, R ColumnRole](field ColumnHandle[M, V, I, P, K, R]) ConflictUpdate[M] {
	c := columnCoreOf(field.stored)
	if c == nil || c.owner == nil {
		return ConflictUpdate[M]{state: conflictUpdate{err: invalid("uninitialized conflict field")}}
	}
	return ConflictUpdate[M]{state: conflictUpdate{owner: c.owner, update: querycore.ConflictUpdate{Field: querycore.FieldRef{Table: c.owner.model.Table, Column: c.metadata.Name}, Incoming: true}}}
}

// Conflict is an immutable model-owned upsert policy; its zero value is invalid.
// Example: orm.OnConflict(orm.ConflictKey(Users.Email)).DoUpdate(orm.ConflictIncoming(Users.Name)).
type Conflict[M any] struct {
	_     [0]*M
	state conflictPolicy
}

type conflictPolicy struct {
	core  querycore.Conflict
	owner *tableCore
	err   error
}

// String omits captured conflict values. Example: fmt.Sprint(policy).
func (Conflict[M]) String() string { return "orm conflict policy" }

// GoString omits captured conflict values. Example: fmt.Sprintf("%#v",policy).
func (Conflict[M]) GoString() string { return "orm conflict policy" }

// OnConflict infers a unique index from typed columns. Empty targets support DO NOTHING.
// Example: orm.OnConflict(orm.ConflictKey(Users.Email)).DoNothing().
func OnConflict[M any](fields ...ConflictColumn[M]) Conflict[M] {
	p := conflictPolicy{}
	for _, field := range fields {
		c := field.column
		if c == nil || c.owner == nil {
			p.err = invalid("uninitialized conflict target")
			continue
		}
		p = p.ownedBy(c.owner)
		p.core.Keys = append(p.core.Keys, querycore.FieldRef{Table: c.owner.model.Table, Column: c.metadata.Name})
	}
	return Conflict[M]{state: p}
}

// OnConflictConstraint selects a named database constraint; PostgreSQL owns its existence.
// Example: orm.OnConflictConstraint[User]("users_email_key").DoNothing().
func OnConflictConstraint[M any](name string) Conflict[M] {
	return Conflict[M]{state: conflictPolicy{core: querycore.Conflict{Named: true, Constraint: name}}}
}

// DoNothing skips conflicting rows; skipped rows have no result.
// Example: orm.OnConflict[User]().DoNothing().
func (p Conflict[M]) DoNothing() Conflict[M] {
	p.state.core.Action = querycore.ConflictNothing
	p.state.core.Updates = nil
	return p
}

// DoUpdate replaces the policy's update assignments. Primary-key updates are rejected.
// Example: policy.DoUpdate(orm.ConflictIncoming(Users.Name)).
func (p Conflict[M]) DoUpdate(updates ...ConflictUpdate[M]) Conflict[M] {
	p.state.core.Action = querycore.ConflictDoUpdate
	p.state.core.Updates = make([]querycore.ConflictUpdate, len(updates))
	for i, u := range updates {
		p.state = p.state.ownedBy(u.state.owner)
		if u.state.err != nil {
			p.state.err = u.state.err
		}
		p.state.core.Updates[i] = u.state.update
	}
	return p
}

// Where adds stored-row conditions; false conditions skip RETURNING but keep row locks.
// Example: policy.Where(orm.Eq(Users.Active, true)).
func (p Conflict[M]) Where(predicates ...Predicate[M]) Conflict[M] {
	p.state.core.Condition = slices.Clone(p.state.core.Condition)
	for _, predicate := range predicates {
		p.state = p.state.ownedBy(predicate.owner)
		if predicate.err != nil {
			p.state.err = predicate.err
		}
		p.state.core.Condition = append(p.state.core.Condition, querycore.PredicateNode(predicate.expression))
	}
	return p
}

func (p conflictPolicy) ownedBy(owner *tableCore) conflictPolicy {
	if owner == nil || p.owner != nil && p.owner != owner {
		p.err = invalid("conflict policy belongs to a different table declaration")
	}
	if p.owner == nil {
		p.owner = owner
	}
	return p
}

// TargetWhere declares a partial unique index predicate, separate from update conditions.
// Fixed target values are rendered as schema constants. Example: policy.TargetWhere(orm.Eq(Users.Active, true)).
func (p Conflict[M]) TargetWhere(predicates ...Predicate[M]) Conflict[M] {
	p.state.core.TargetCondition = slices.Clone(p.state.core.TargetCondition)
	for _, predicate := range predicates {
		p.state = p.state.ownedBy(predicate.owner)
		if predicate.err != nil {
			p.state.err = predicate.err
		}
		p.state.core.TargetCondition = append(p.state.core.TargetCondition, querycore.PredicateNode(predicate.expression))
	}
	return p
}
