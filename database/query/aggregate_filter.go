package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// The immutable pointer keeps aggregateNode comparable for zero-declaration
// checks while storing ordinary predicates in the shared expression AST.
type aggregateFilter struct{ predicates []expression }

// Filter appends row predicates to this aggregate only, joined with AND. It
// preserves the aggregate's codec and empty-input behavior. Query Where still
// filters input for every aggregate; Having filters completed groups. Apply
// Filter before Over when evaluating a filtered aggregate over a window.
func (a Aggregate[M, V]) Filter(predicates ...Predicate[M]) Aggregate[M, V] {
	if len(predicates) == 0 {
		return a
	}
	var existing []expression
	if a.node.filter != nil {
		existing = a.node.filter.predicates
	}
	a.node.filter = &aggregateFilter{predicates: appendPredicates(existing, predicates)}
	return a
}

// Filter retains ordered comparisons such as Gt and Gte on a filtered count.
func (a OrderedAggregate[M, V]) Filter(predicates ...Predicate[M]) OrderedAggregate[M, V] {
	a.Aggregate = a.Aggregate.Filter(predicates...)
	return a
}

// Filter retains concrete-value comparisons and nullable result decoding.
func (a NullableOrderedAggregate[M, V]) Filter(predicates ...Predicate[M]) NullableOrderedAggregate[M, V] {
	a.Aggregate = a.Aggregate.Filter(predicates...)
	return a
}

func (a aggregateNode) validateFilter(field func(fieldRef) error, nodes *int) error {
	if a.filter == nil {
		return nil
	}
	if len(a.filter.predicates) == 0 || len(a.filter.predicates) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "aggregate filter requires bounded row predicates")
	}
	for _, predicate := range a.filter.predicates {
		if err := validateExpressionFields(predicate, field, 0, nodes); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) aggregateFilterSQL(a aggregateNode, grouped map[fieldRef]bool, grouping, window bool) (string, error) {
	if a.filter == nil {
		return "", nil
	}
	// PostgreSQL can move an ordinary aggregate to an enclosing SELECT when
	// all argument/filter references belong there. Observe resolved references
	// at this level, including those captured through nested correlations;
	// fields inside independent subqueries do not count as local inputs.
	local, outer := a.field != (fieldRef{}), false
	if !window {
		previous, level, sources := c.fieldObserver, c.selectDepth, c.sources
		c.fieldObserver = func(depth int, f fieldRef) {
			if previous != nil {
				previous(depth, f)
			}
			if depth == level {
				if sources[f.table] != nil {
					local = true
				} else {
					outer = true
				}
			}
		}
		defer func() { c.fieldObserver = previous }()
	}
	var sql strings.Builder
	if err := c.conditionClauseAt(&sql, "WHERE", a.filter.predicates, grouped, grouping); err != nil {
		return "", err
	}
	if outer && !local {
		return "", fault.New(fault.Invalid, "aggregate filter requires a local input; outer-only references change SQL query ownership")
	}
	return " FILTER (" + strings.TrimPrefix(sql.String(), " ") + ")", nil
}

func requalifyAggregate(a aggregateNode, alias string) aggregateNode {
	if a.field != (fieldRef{}) {
		a.field.table = alias
	}
	if a.filter != nil {
		predicates := make([]expression, len(a.filter.predicates))
		for i, predicate := range a.filter.predicates {
			predicates[i] = requalify(predicate, alias)
		}
		a.filter = &aggregateFilter{predicates: predicates}
	}
	return a
}
