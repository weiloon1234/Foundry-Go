package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"strings"
)

// The existing SELECT compiler owns source scopes, CTE dependencies, expression
// validation and bindings. Destination declaration order owns insertion columns.
// This private compiler accepts only already-prepared literal values.
func (q QueryCore) CompileInsertSelect(c Compiler, node SelectNode, mapped map[string]ValueExpression, values map[string]MutationAssignment, kind func(string) codec.ParameterType, returning bool) (Statement, error) {
	c.BeginStatement(q.ScopeContext)
	node.Selections = make([]SelectItem, 0, len(mapped)+len(values))
	columns := make([]string, 0, cap(node.Selections))
	for _, column := range q.Model.Columns {
		var expression ValueExpression
		if mapping, ok := mapped[column.Name]; ok {
			expression = mapping
		} else if assignment, ok := values[column.Name]; ok {
			bound, err := BindAssignment(column, assignment.Bind)
			if err != nil {
				return Statement{}, err
			}
			expression = ParameterNode{Kind: kind(column.Name), Value: RetainLiteral(bound)}
		} else {
			continue
		}
		columns = append(columns, Quoted(column.Name))
		node.Selections = append(node.Selections, SelectItem{Expression: expression, Alias: column.Name})
	}
	prefix, err := c.CompileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	selected, err := c.SelectSQL(node)
	if err != nil {
		return Statement{}, err
	}
	sql := prefix + "INSERT INTO " + QuotedTable(q.Table) + " (" + strings.Join(columns, ", ") + ") " + selected
	if returning {
		sql += " RETURNING " + SelectedColumns(q.Table, q.Model.Columns)
	}
	return c.Statement(sql), nil
}
