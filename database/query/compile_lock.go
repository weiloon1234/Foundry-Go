package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Lock clauses belong to their SELECT node. The compiler permits them only
// through a transaction-required read boundary, including nested definitions.
func compileLock(node selectNode, spec lockSpec) (string, error) {
	if spec.strength < lockUpdate || spec.strength > lockKeyShare || spec.behavior > lockSkip {
		return "", fault.New(fault.Invalid, "invalid row lock strength or wait policy")
	}
	if spec.explicit && (len(spec.targets) == 0 || len(spec.targets) > MaxExpressionNodes) {
		return "", fault.New(fault.Invalid, "row lock Of requires a bounded, nonempty source list")
	}
	if n, err := validateLockSources(node, spec.targets, spec.explicit, 0); err != nil {
		return "", err
	} else if n == 0 {
		return "", fault.New(fault.Invalid, "row lock requires at least one underlying table; CTE rows are not locked by an outer SELECT")
	}
	text := " FOR " + [...]string{"", "UPDATE", "NO KEY UPDATE", "SHARE", "KEY SHARE"}[spec.strength]
	if spec.explicit {
		// PostgreSQL OF takes an alias or bare relation name, never a qualified
		// schema.table identifier. Reject ambiguous names before execution.
		sources, _ := lockSources(node)
		names := make(map[string]int, len(sources))
		for _, source := range sources {
			names[lockReference(source.name())]++
		}
		references := make([]string, len(spec.targets))
		for i, target := range spec.targets {
			name := lockReference(target)
			if names[name] != 1 {
				return "", fault.New(fault.Invalid, "ambiguous row lock target; alias the input tables")
			}
			references[i] = quoted(name)
		}
		text += " OF " + strings.Join(references, ", ")
	}
	text += [...]string{"", " NOWAIT", " SKIP LOCKED"}[spec.behavior]
	return text, nil
}

func (c *compiler) compileLocks(node selectNode, specs []lockSpec) (string, error) {
	if len(specs) == 0 || len(specs) > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "row lock clauses exceed their resource bound")
	}
	// Every clause examines the same query. Bound their combined validation work
	// before repeating source/selection validation across independently scoped clauses.
	var walk selectWalk
	walk.selectNode(node, 0)
	if walk.err != nil {
		return "", walk.err
	}
	if len(specs) > MaxExpressionNodes/max(1, walk.nodes) {
		return "", fault.New(fault.Invalid, "combined row lock validation exceeds its resource bound")
	}
	work := len(specs) * max(1, walk.nodes)
	if work > MaxExpressionNodes-c.lockWork {
		return "", fault.New(fault.Invalid, "nested row lock validation exceeds its resource bound")
	}
	c.lockWork += work
	var sql strings.Builder
	for _, spec := range specs {
		text, err := compileLock(node, spec)
		if err != nil {
			return "", err
		}
		sql.WriteString(text)
	}
	return sql.String(), nil
}

func lockReference(table string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[i+1:]
	}
	return table
}

// Called only after the shared SELECT compiler has validated the entire AST and
// its resource bounds. Predicate/scalar subqueries are independent lock scopes.
func validateLockSources(node selectNode, targets []string, explicit bool, depth int) (int, error) {
	if depth > MaxExpressionDepth {
		return 0, fault.New(fault.Invalid, "row lock source exceeds its depth bound")
	}
	if node.distinct.kind != noDistinct || len(node.groupBy) != 0 || len(node.having) != 0 {
		return 0, fault.New(fault.Invalid, "row locking cannot select distinct or grouped results")
	}
	for _, selection := range node.selections {
		if err := validateLockValue(selection.expression); err != nil {
			return 0, err
		}
	}
	for _, order := range node.orders {
		if err := validateLockValue(order.expression); err != nil {
			return 0, err
		}
	}
	sources, nullable := lockSources(node)
	byName := make(map[string]int, len(sources))
	for i, source := range sources {
		byName[source.name()] = i
	}
	selected := make(map[string]bool, len(targets))
	for _, target := range targets {
		if _, ok := byName[target]; !ok || selected[target] {
			return 0, fault.New(fault.Invalid, "row lock target is unknown or repeated")
		}
		selected[target] = true
	}
	tables := 0
	for i, source := range sources {
		if explicit && !selected[source.name()] {
			continue
		}
		if source.cte != nil || source.self != nil {
			if explicit {
				return 0, fault.New(fault.Invalid, "row lock Of cannot target a CTE reference")
			}
			continue // PostgreSQL does not propagate outer locks into WITH queries.
		}
		if nullable[i] {
			return 0, fault.New(fault.Invalid, "row locking cannot target the nullable side of an outer join; use Of with a preserved scope")
		}
		if source.set != nil {
			return 0, fault.New(fault.Invalid, "row locking cannot target set-operation results")
		}
		if source.query != nil {
			n, err := validateLockSources(*source.query, nil, false, depth+1)
			if err != nil {
				return 0, err
			}
			if explicit && n == 0 {
				return 0, fault.New(fault.Invalid, "row lock Of target has no lockable underlying table")
			}
			tables += n
		} else {
			tables++
		}
	}
	return tables, nil
}

// Calculations can contain windows below their root node. Reuse the bounded
// expression visitor, keeping scalar subqueries in their independent SELECT.
func validateLockValue(v valueExpression) error {
	if groupsSelect(v) {
		return fault.New(fault.Invalid, "row locking cannot select or order by aggregate results")
	}
	walk := selectWalk{localSelect: true}
	walk.window = func(windowNode) {
		walk.err = fault.New(fault.Invalid, "row locking cannot select or order by window results")
	}
	walk.value(v, 0)
	return walk.err
}
