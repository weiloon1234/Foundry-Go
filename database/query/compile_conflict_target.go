package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p Conflict[M]) validateTarget(q Query[M], c *compiler) error {
	if len(p.targetCondition) > 0 && (p.named || len(p.keys) == 0) {
		return fault.New(fault.Invalid, "partial conflict targets require index keys")
	}
	nodes := 0
	var walk selectWalk
	walk.Source = func(tableSource, int) {
		walk.Err = fault.New(fault.Invalid, "conflict index targets cannot contain subqueries")
	}
	field := func(f fieldRef) error {
		if err := f.Validate(q.queryCore.Table); err != nil {
			return err
		}
		return c.DeclaredField(f)
	}
	for _, key := range p.keys {
		if err := validateRowValue(key, field, 0, &nodes); err != nil {
			return err
		}
		walk.Value(key, 0)
		if walk.Err != nil {
			return walk.Err
		}
	}
	for _, predicate := range p.targetCondition {
		if err := validateExpressionFields(predicate, field, 0, &nodes); err != nil {
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
func (p Conflict[M]) targetSQL(parent *compiler, table string) (string, error) {
	if p.named {
		return " ON CONSTRAINT " + quoted(p.constraint), nil
	}
	if len(p.keys) == 0 {
		return "", nil
	}
	c := *parent
	c.IndexTarget = true
	c.Arguments = nil
	c.Keys = nil
	c.Sources = map[string]map[string]columnData{table: c.Columns}
	defer func() {
		parent.ExpressionNodes = c.ExpressionNodes
		parent.SelectNodes = c.SelectNodes
		parent.ScalarSQLBytes = c.ScalarSQLBytes
	}()
	var index valueIndex
	keys := make([]string, len(p.keys))
	for i, key := range p.keys {
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
		if _, field := key.(fieldRef); !field {
			keys[i] = "(" + keys[i] + ")"
		}
	}
	var result strings.Builder
	result.WriteString(" (" + strings.Join(keys, ", ") + ")")
	if err := c.Where(&result, p.targetCondition); err != nil {
		return "", err
	}
	if result.Len() > MaxScalarSQLBytes {
		return "", fault.New(fault.Invalid, "conflict target SQL exceeds its resource bound")
	}
	return result.String(), nil
}
