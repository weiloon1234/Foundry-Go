package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Lock clauses belong to their SELECT node. The compiler permits them only
// through a transaction-required read boundary, including nested definitions.
func CompileLock(node SelectNode, spec LockSpec) (string, error) {
	if spec.Strength < LockUpdate || spec.Strength > LockKeyShare || spec.Behavior > LockSkip {
		return "", fault.New(fault.Invalid, "invalid row lock strength or wait policy")
	}
	if spec.Explicit && (len(spec.Targets) == 0 || len(spec.Targets) > MaxExpressionNodes) {
		return "", fault.New(fault.Invalid, "row lock Of requires a bounded, nonempty source list")
	}
	if n, err := ValidateLockSources(node, spec.Targets, spec.Explicit, 0); err != nil {
		return "", err
	} else if n == 0 {
		return "", fault.New(fault.Invalid, "row lock requires at least one underlying table; CTE rows are not locked by an outer SELECT")
	}
	text := " FOR " + [...]string{"", "UPDATE", "NO KEY UPDATE", "SHARE", "KEY SHARE"}[spec.Strength]
	if spec.Explicit {
		// PostgreSQL OF takes an alias or bare relation name, never a qualified
		// schema.table identifier. Reject ambiguous names before execution.
		sources, _ := LockSources(node)
		names := make(map[string]int, len(sources))
		for _, source := range sources {
			names[LockReference(source.Name())]++
		}
		references := make([]string, len(spec.Targets))
		for i, target := range spec.Targets {
			name := LockReference(target)
			if names[name] != 1 {
				return "", fault.New(fault.Invalid, "ambiguous row lock target; alias the input tables")
			}
			references[i] = Quoted(name)
		}
		text += " OF " + strings.Join(references, ", ")
	}
	text += [...]string{"", " NOWAIT", " SKIP LOCKED"}[spec.Behavior]
	return text, nil
}

func (c *Compiler) CompileLocks(node SelectNode, specs []LockSpec) (string, error) {
	if len(specs) == 0 || len(specs) > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "row lock clauses exceed their resource bound")
	}
	// Every clause examines the same query. Bound their combined validation work
	// before repeating source/selection validation across independently scoped clauses.
	var walk SelectWalk
	walk.SelectNode(node, 0)
	if walk.Err != nil {
		return "", walk.Err
	}
	if len(specs) > MaxExpressionNodes/max(1, walk.Nodes) {
		return "", fault.New(fault.Invalid, "combined row lock validation exceeds its resource bound")
	}
	work := len(specs) * max(1, walk.Nodes)
	if work > MaxExpressionNodes-c.LockWork {
		return "", fault.New(fault.Invalid, "nested row lock validation exceeds its resource bound")
	}
	c.LockWork += work
	var sql strings.Builder
	for _, spec := range specs {
		text, err := CompileLock(node, spec)
		if err != nil {
			return "", err
		}
		sql.WriteString(text)
	}
	return sql.String(), nil
}

func LockReference(table string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[i+1:]
	}
	return table
}

// Called only after the shared SELECT compiler has validated the entire AST and
// its resource bounds. Predicate/scalar subqueries are independent lock scopes.
func ValidateLockSources(node SelectNode, targets []string, explicit bool, depth int) (int, error) {
	if depth > MaxExpressionDepth {
		return 0, fault.New(fault.Invalid, "row lock source exceeds its depth bound")
	}
	if node.Distinct.Kind != NoDistinct || len(node.GroupBy) != 0 || len(node.Having) != 0 {
		return 0, fault.New(fault.Invalid, "row locking cannot select distinct or grouped results")
	}
	for _, selection := range node.Selections {
		if err := ValidateLockValue(selection.Expression); err != nil {
			return 0, err
		}
	}
	for _, order := range node.Orders {
		if err := ValidateLockValue(order.Expression); err != nil {
			return 0, err
		}
	}
	sources, nullable := LockSources(node)
	byName := make(map[string]int, len(sources))
	for i, source := range sources {
		byName[source.Name()] = i
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
		if explicit && !selected[source.Name()] {
			continue
		}
		if source.Cte != nil || source.Self != nil {
			if explicit {
				return 0, fault.New(fault.Invalid, "row lock Of cannot target a CTE reference")
			}
			continue // PostgreSQL does not propagate outer locks into WITH queries.
		}
		if nullable[i] {
			return 0, fault.New(fault.Invalid, "row locking cannot target the nullable side of an outer join; use Of with a preserved scope")
		}
		if source.Set != nil {
			return 0, fault.New(fault.Invalid, "row locking cannot target set-operation results")
		}
		if source.Query != nil {
			n, err := ValidateLockSources(*source.Query, nil, false, depth+1)
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
func ValidateLockValue(v ValueExpression) error {
	if GroupsSelect(v) {
		return fault.New(fault.Invalid, "row locking cannot select or order by aggregate results")
	}
	walk := SelectWalk{LocalSelect: true}
	walk.Window = func(WindowNode) {
		walk.Err = fault.New(fault.Invalid, "row locking cannot select or order by window results")
	}
	walk.Value(v, 0)
	return walk.Err
}
