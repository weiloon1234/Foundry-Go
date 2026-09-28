package query

import (
	"slices"
)

type conflictColumn[M any] struct {
	_     [0]*M
	field fieldRef
}

// ConflictField is a generated field owned by the upsert's model. Fields of
// different value types can form one composite target without losing ownership.
type ConflictField[M any] interface{ conflictColumn() conflictColumn[M] }

func (f valueField[M, V]) conflictColumn() conflictColumn[M] {
	return conflictColumn[M]{field: f.ref}
}

// ConflictUpdate assigns a typed constant, calculation or proposed column.
// Use generated fields' Set/SetNull/Incoming methods or SetConflictValue.
type ConflictUpdate[M any] struct {
	_              [0]*M
	field          fieldRef
	literal        *assignmentValue
	mutatedLiteral bool // Private phase marker: literal now contains the mutator output.
	incoming       bool
	null           bool
	value          valueExpression
}

// Incoming copies this field from PostgreSQL's proposed (EXCLUDED) row,
// including database defaults and BEFORE INSERT trigger changes.
func (f valueField[M, V]) Incoming() ConflictUpdate[M] {
	return ConflictUpdate[M]{field: f.ref, incoming: true}
}

// Set assigns a concrete typed value when the insert conflicts.
func (f valueField[M, V]) Set(v V) ConflictUpdate[M] {
	return literalConflict[M](f.ref, captureAssignment(f.codec, v))
}

// SetNull assigns SQL NULL to a nullable field on conflict.
func (f NullableField[M, V]) SetNull() ConflictUpdate[M] {
	return ConflictUpdate[M]{field: f.ref, null: true}
}

func literalConflict[M any](field fieldRef, value assignmentValue) ConflictUpdate[M] {
	return ConflictUpdate[M]{field: field, literal: &value}
}

type conflictAction uint8

const (
	conflictUnset conflictAction = iota
	conflictNothing
	conflictUpdate
)

// Conflict is an immutable model-owned PostgreSQL conflict policy. The zero
// value has no action and is invalid. Physical unique constraints remain owned
// by migrations and are validated by PostgreSQL, not by these Go declarations.
type Conflict[M any] struct {
	_               [0]*M
	keys            []valueExpression
	constraint      string
	named           bool
	action          conflictAction
	updates         []ConflictUpdate[M]
	condition       []expression
	rowCondition    []expression
	targetCondition []expression
}

// OnConflict infers a unique index from generated fields. With no fields,
// OnConflict[Model]().DoNothing() handles every eligible unique conflict.
func OnConflict[M any](fields ...ConflictField[M]) Conflict[M] {
	c := Conflict[M]{keys: make([]valueExpression, len(fields))}
	for i, f := range fields {
		if !nilDescriptor(f) {
			c.keys[i] = f.conflictColumn().field
		}
	}
	return c
}

// OnConflictConstraint names a model-owned database constraint explicitly.
// Prefer OnConflict with fields when index inference can express the target.
func OnConflictConstraint[M any](name string) Conflict[M] {
	return Conflict[M]{constraint: name, named: true}
}

// DoNothing skips conflicts. Conditional update predicates are not applicable.
func (c Conflict[M]) DoNothing() Conflict[M] { c.action = conflictNothing; c.updates = nil; return c }

// DoUpdate selects the conflict assignments, replacing any previous selection.
func (c Conflict[M]) DoUpdate(updates ...ConflictUpdate[M]) Conflict[M] {
	c.action = conflictUpdate
	c.updates = slices.Clone(updates)
	return c
}

// Update copies selected fields from the proposed row. Other stored fields stay
// unchanged. Selected omitted fields copy their database default or NULL.
func (c Conflict[M]) Update(fields ...ConflictField[M]) Conflict[M] {
	updates := make([]ConflictUpdate[M], len(fields))
	for i, f := range fields {
		if !nilDescriptor(f) {
			updates[i] = ConflictUpdate[M]{field: f.conflictColumn().field, incoming: true}
		}
	}
	return c.DoUpdate(updates...)
}

// Where conditionally updates the existing row after a conflict is found. A
// false condition produces no returned row; it does not undo PostgreSQL's lock.
func (c Conflict[M]) Where(predicates ...Predicate[M]) Conflict[M] {
	c.condition = appendPredicates(c.condition, predicates)
	return c
}

func appendPredicates[M any](base []expression, predicates []Predicate[M]) []expression {
	base = slices.Clone(base)
	for _, p := range predicates {
		base = append(base, p.expression)
	}
	return base
}
