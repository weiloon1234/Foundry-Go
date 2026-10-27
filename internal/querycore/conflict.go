package querycore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Conflict contains SQL policy data only; public facades retain model ownership.
type Conflict struct {
	Keys                                     []ValueExpression
	Constraint                               string
	Named                                    bool
	Action                                   ConflictAction
	Updates                                  []ConflictUpdate
	Condition, RowCondition, TargetCondition []Expression
}

type ConflictAction uint8

const (
	_ ConflictAction = iota
	ConflictNothing
	ConflictDoUpdate
)

type ConflictUpdate struct {
	Field          FieldRef
	Literal        *AssignmentValue
	MutatedLiteral bool
	Incoming, Null bool
	Value          ValueExpression
}

const ConflictStoredTable = "foundry_conflict_stored"

// WithUpdatedTimestamp copies the normalized proposed instant during DO UPDATE.
// Example: policy.WithUpdatedTimestamp("users", "updated_at").
func (p Conflict) WithUpdatedTimestamp(table, name string) Conflict {
	if p.Action != ConflictDoUpdate {
		return p
	}
	p.Updates = slices.Clone(p.Updates)
	update := ConflictUpdate{Field: FieldRef{Table: table, Column: name}, Incoming: true}
	i := slices.IndexFunc(p.Updates, func(u ConflictUpdate) bool { return u.Field.Column == name })
	if i < 0 {
		p.Updates = append(p.Updates, update)
	} else {
		p.Updates[i] = update
	}
	return p
}

// CheckReturned rejects an unconditional update suppressed by destination scopes.
// Example: policy.CheckReturned(query, 2, 1).
func (p Conflict) CheckReturned(q QueryCore, requested, returned int) error {
	if p.Action != ConflictDoUpdate || len(p.Condition) != 0 || len(p.RowCondition) != 0 || returned >= requested || len(q.ScopePredicates()) == 0 {
		return nil
	}
	return safeerror.NewFault(fault.Conflict, "upsert conflicts with a row outside the model's global scopes")
}
