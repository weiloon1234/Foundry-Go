package validation

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// slotKey has a nonzero size so every declared slot has a distinct address.
type slotKey struct{ _ byte }

// Slot declares a typed value that an enclosing Provide derives once per check
// from the request context and the input it validates, such as the key of the
// row being updated or the tenant of the authenticated actor. Rules and lookups
// below that Provide read it with Value, so a rule tree is declared once and
// parameterized at check time instead of being rebuilt for every request.
// Declare slots once, beside the rules that use them. The zero Slot is invalid.
type Slot[P any] struct{ key *slotKey }

func NewSlot[P any]() Slot[P] { return Slot[P]{key: new(slotKey)} }

type provided[P any] struct{ value P }

// Value returns the value provided for the current check. Outside a Provide for
// this slot it returns an error, never a zero value, so a lookup scoped by the
// slot cannot silently widen or skip its predicate.
func (s Slot[P]) Value(ctx context.Context) (P, error) {
	var zero P
	if s.key == nil || ctx == nil {
		return zero, invalid("validation slot is not defined")
	}
	current, ok := ctx.Value(s.key).(provided[P])
	if !ok {
		return zero, invalid("validation slot requires an enclosing Provide")
	}
	return current.value, nil
}

// Provide derives the slot's value from the check context and the input at this
// level, then runs rules with that value available to their contexts. For HTTP,
// attach it with WithValidation so derive can read path parameters while the
// rules select body fields. derive is an application callback: its error is an
// execution failure, never a rejection or an absent value. The value is visible
// only to this subtree and only for the current check.
func Provide[T, P any](slot Slot[P], derive func(context.Context, T) (P, error), rules ...Rule[T]) Rule[T] {
	if slot.key == nil || derive == nil {
		return failed[T](invalid("validation slot provider requires a declared slot and derive callback"))
	}
	child := All(rules...)
	rule := lift(Description{Kind: AllKind}, child, func(s *execution, input T, depth int) {
		value, err := derive(s.ctx, input)
		if canceled := s.ctx.Err(); canceled != nil {
			s.err = canceled
			return
		}
		if err != nil {
			s.err = fault.Wrap(fault.Internal, "validation slot provider failed", err)
			return
		}
		previous := s.ctx
		s.ctx = context.WithValue(previous, slot.key, provided[P]{value: value})
		child.run(s, input, depth+1)
		s.ctx = previous
	})
	if rule.err != nil {
		return rule
	}
	rule.callbacks = true
	rule.slots = slices.DeleteFunc(slices.Clone(rule.slots), func(key *slotKey) bool { return key == slot.key })
	return rule
}

// Requires records that rule reads slot. Validate then rejects any tree in which
// no enclosing Provide supplies it, so a missing provider fails registration
// instead of every check. Adapters that accept a Slot apply this themselves.
func Requires[T, P any](slot Slot[P], rule Rule[T]) Rule[T] {
	if slot.key == nil {
		return failed[T](invalid("validation slot is not defined"))
	}
	if err := rule.validateStructure(); err != nil {
		return rule
	}
	if !slices.Contains(rule.slots, slot.key) {
		rule.slots = append(slices.Clone(rule.slots), slot.key)
	}
	return rule
}
