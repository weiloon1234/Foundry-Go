package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// The immutable pointer keeps aggregateNode comparable for zero-declaration
// checks while storing ordinary predicates in the shared expression AST.
type AggregateFilter struct{ Predicates []Expression }

func (a AggregateNode) ValidateFilter(field func(FieldRef) error, nodes *int) error {
	if a.Filter == nil {
		return nil
	}
	if len(a.Filter.Predicates) == 0 || len(a.Filter.Predicates) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "aggregate filter requires bounded row predicates")
	}
	for _, predicate := range a.Filter.Predicates {
		if err := ValidateExpressionFields(predicate, field, 0, nodes); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) AggregateFilterSQL(a AggregateNode, grouped map[FieldRef]bool, grouping, window bool) (string, error) {
	if a.Filter == nil {
		return "", nil
	}
	// PostgreSQL can move an ordinary aggregate to an enclosing SELECT when
	// all argument/filter references belong there. Observe resolved references
	// at this level, including those captured through nested correlations;
	// fields inside independent subqueries do not count as local inputs.
	local, outer := a.Field != (FieldRef{}), false
	if !window {
		previous, level, sources := c.FieldObserver, c.SelectDepth, c.Sources
		c.FieldObserver = func(depth int, f FieldRef) {
			if previous != nil {
				previous(depth, f)
			}
			if depth == level {
				if sources[f.Table] != nil {
					local = true
				} else {
					outer = true
				}
			}
		}
		defer func() { c.FieldObserver = previous }()
	}
	var sql strings.Builder
	if err := c.ConditionClauseAt(&sql, "WHERE", a.Filter.Predicates, grouped, grouping); err != nil {
		return "", err
	}
	if outer && !local {
		return "", fault.New(fault.Invalid, "aggregate filter requires a local input; outer-only references change SQL query ownership")
	}
	return " FILTER (" + strings.TrimPrefix(sql.String(), " ") + ")", nil
}
