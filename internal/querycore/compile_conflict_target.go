package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p Conflict) validateTarget(q QueryCore, c *Compiler) error {
	if len(p.TargetCondition) > 0 && (p.Named || len(p.Keys) == 0) {
		return fault.New(fault.Invalid, "partial conflict targets require index keys")
	}
	nodes := 0
	var walk SelectWalk
	walk.Source = func(TableSource, int) {
		walk.Err = fault.New(fault.Invalid, "conflict index targets cannot contain subqueries")
	}
	field := func(f FieldRef) error {
		if err := f.Validate(q.Table); err != nil {
			return err
		}
		return c.DeclaredField(f)
	}
	for _, key := range p.Keys {
		if err := ValidateRowValue(key, field, 0, &nodes); err != nil {
			return err
		}
		walk.Value(key, 0)
		if walk.Err != nil {
			return walk.Err
		}
	}
	for _, predicate := range p.TargetCondition {
		if err := ValidateExpressionFields(predicate, field, 0, &nodes); err != nil {
			return err
		}
		walk.Expression(predicate, 0)
		if walk.Err != nil {
			return walk.Err
		}
	}
	return nil
}

// Index inference is a schema boundary. Reuse the expression compiler with a
// local constant/column rendering mode; execution clauses retain the original
// compiler and its uninterrupted parameter numbering.
func (p Conflict) targetSQL(parent *Compiler, table string) (string, error) {
	if p.Named {
		return " ON CONSTRAINT " + Quoted(p.Constraint), nil
	}
	if len(p.Keys) == 0 {
		return "", nil
	}
	c := *parent
	c.IndexTarget = true
	c.Arguments = nil
	c.Keys = nil
	c.Sources = map[string]map[string]Column{table: c.Columns}
	defer func() {
		parent.ExpressionNodes = c.ExpressionNodes
		parent.SelectNodes = c.SelectNodes
		parent.ScalarSQLBytes = c.ScalarSQLBytes
	}()
	var index ValueIndex
	keys := make([]string, len(p.Keys))
	for i, key := range p.Keys {
		planned, err := c.PlanValue(key)
		if err != nil {
			return "", err
		}
		if _, duplicate, err := index.Find(planned); err != nil {
			return "", err
		} else if duplicate {
			return "", fault.New(fault.Invalid, "conflict target repeats an index key")
		}
		index.Add(planned)
		keys[i] = planned.Sql
		if _, field := key.(FieldRef); !field {
			keys[i] = "(" + keys[i] + ")"
		}
	}
	var result strings.Builder
	result.WriteString(" (" + strings.Join(keys, ", ") + ")")
	if err := c.Where(&result, p.TargetCondition); err != nil {
		return "", err
	}
	if result.Len() > MaxScalarSQLBytes {
		return "", fault.New(fault.Invalid, "conflict target SQL exceeds its resource bound")
	}
	return result.String(), nil
}
