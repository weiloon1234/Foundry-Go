package querycore

import (
	"context"
	"database/sql/driver"
	"slices"
)

// LookupDefault is a stored equality value eligible as an omitted creation input.
type LookupDefault struct {
	Column string
	Value  driver.Value
}

// LookupDefaults resolves top-level equalities and scopes, excluding conflicts and mutators.
func LookupDefaults(ctx context.Context, q QueryCore, eligible func(string) bool) ([]LookupDefault, error) {
	values := make(map[string]driver.Value)
	conflicted := make(map[string]bool)
	var order []string
	var collect func(Expression) error
	collect = func(e Expression) error {
		switch e := e.(type) {
		case Junction:
			if e.Any {
				return nil
			}
			for _, child := range e.Children {
				if err := collect(child); err != nil {
					return err
				}
			}
		case ScopeNode:
			resolved, err := e.Resolved(ctx)
			if err != nil {
				return err
			}
			return collect(resolved)
		case Comparison:
			field, ok := UnwrapSensitivity(e.Operand).(FieldRef)
			if !ok || field.Table != q.Table || e.Operator != Equal || len(e.Values) != 1 || e.Bind == nil {
				return nil
			}
			if !eligible(field.Column) {
				return nil
			}
			raw, err := e.Bind(e.Values[0])
			if err != nil || raw == nil {
				return err
			}
			if previous, seen := values[field.Column]; seen {
				if !LookupValuesEqual(previous, raw) {
					conflicted[field.Column] = true
				}
				return nil
			}
			values[field.Column] = raw
			order = append(order, field.Column)
		}
		return nil
	}
	for _, e := range append(slices.Clone(q.Predicates), q.ScopePredicates()...) {
		if err := collect(e); err != nil {
			return nil, err
		}
	}
	defaults := make([]LookupDefault, 0, len(order))
	for _, column := range order {
		if !conflicted[column] {
			defaults = append(defaults, LookupDefault{column, values[column]})
		}
	}
	return defaults, nil
}

// LookupValuesEqual compares stored equality defaults with canonical model-key semantics.
func LookupValuesEqual(a, b driver.Value) bool {
	left, le := EncodeModelKey(a)
	right, re := EncodeModelKey(b)
	return le == nil && re == nil && left == right
}
