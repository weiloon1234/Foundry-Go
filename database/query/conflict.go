package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type conflictColumn[M any] struct {
	_     [0]*M
	field fieldRef
}

// ConflictField is a generated field owned by the upsert's model. Fields of
// different value types can form one composite target without losing ownership.
type ConflictField[M any] interface{ conflictColumn() conflictColumn[M] }

func (f ScalarField[M, V]) conflictColumn() conflictColumn[M] {
	return conflictColumn[M]{field: f.ref}
}

// ConflictUpdate assigns a typed constant, calculation or proposed column.
// Use generated fields' Set/SetNull/Incoming methods or SetConflictValue.
type ConflictUpdate[M any] struct {
	_      [0]*M
	update conflictUpdate
}

type conflictUpdate = querycore.ConflictUpdate

// Incoming copies this field from PostgreSQL's proposed (EXCLUDED) row,
// including database defaults and BEFORE INSERT trigger changes.
func (f ScalarField[M, V]) Incoming() ConflictUpdate[M] {
	return ConflictUpdate[M]{update: conflictUpdate{Field: f.ref, Incoming: true}}
}

// Set assigns a concrete typed value when the insert conflicts.
func (f ScalarField[M, V]) Set(v V) ConflictUpdate[M] {
	return literalConflict[M](f.ref, captureAssignment(f.codec, v))
}

// SetNull assigns SQL NULL to a nullable field on conflict.
func (f NullableField[M, V]) SetNull() ConflictUpdate[M] {
	return ConflictUpdate[M]{update: conflictUpdate{Field: f.ref, Null: true}}
}

func literalConflict[M any](field fieldRef, value assignmentValue) ConflictUpdate[M] {
	return ConflictUpdate[M]{update: conflictUpdate{Field: field, Literal: &value}}
}

// Conflict is an immutable model-owned PostgreSQL conflict policy. The zero
// value has no action and is invalid. Physical unique constraints remain owned
// by migrations and are validated by PostgreSQL, not by these Go declarations.
type Conflict[M any] struct {
	_     [0]*M
	state querycore.Conflict
}

// OnConflict infers a unique index from generated fields. With no fields,
// OnConflict[Model]().DoNothing() handles every eligible unique conflict.
func OnConflict[M any](fields ...ConflictField[M]) Conflict[M] {
	c := Conflict[M]{state: querycore.Conflict{Keys: make([]valueExpression, len(fields))}}
	for i, f := range fields {
		if !nilDescriptor(f) {
			c.state.Keys[i] = f.conflictColumn().field
		}
	}
	return c
}

// OnConflictConstraint names a model-owned database constraint explicitly.
// Prefer OnConflict with fields when index inference can express the target.
func OnConflictConstraint[M any](name string) Conflict[M] {
	return Conflict[M]{state: querycore.Conflict{Constraint: name, Named: true}}
}

// DoNothing skips conflicts. Conditional update predicates are not applicable.
func (c Conflict[M]) DoNothing() Conflict[M] {
	c.state.Action = querycore.ConflictNothing
	c.state.Updates = nil
	return c
}

// DoUpdate selects the conflict assignments, replacing any previous selection.
func (c Conflict[M]) DoUpdate(updates ...ConflictUpdate[M]) Conflict[M] {
	c.state.Action = querycore.ConflictDoUpdate
	c.state.Updates = make([]conflictUpdate, len(updates))
	for i, update := range updates {
		c.state.Updates[i] = update.update
	}
	return c
}

// Update copies selected fields from the proposed row. Other stored fields stay
// unchanged. Selected omitted fields copy their database default or NULL.
func (c Conflict[M]) Update(fields ...ConflictField[M]) Conflict[M] {
	c.state.Action = querycore.ConflictDoUpdate
	c.state.Updates = make([]conflictUpdate, len(fields))
	for i, field := range fields {
		if !nilDescriptor(field) {
			c.state.Updates[i] = conflictUpdate{Field: field.conflictColumn().field, Incoming: true}
		}
	}
	return c
}

// Where conditionally updates the existing row after a conflict is found. A
// false condition produces no returned row; it does not undo PostgreSQL's lock.
func (c Conflict[M]) Where(predicates ...Predicate[M]) Conflict[M] {
	c.state.Condition = appendPredicates(c.state.Condition, predicates)
	return c
}

func appendPredicates[M any](base []expression, predicates []Predicate[M]) []expression {
	base = slices.Clone(base)
	for _, p := range predicates {
		base = append(base, predicateExpression(p.expression))
	}
	return base
}
