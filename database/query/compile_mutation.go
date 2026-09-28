package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p mutationPlan[M]) compile() (Statement, error) {
	q := p.query
	if p.kind == insertModel {
		return (insertPlan[M]{query: q, rows: []Mutation[M]{p.mutation}}).compile()
	}
	c, err := q.mutationCompiler(p.kind)
	if err != nil {
		return Statement{}, err
	}
	assigned, err := q.validateMutation(p.kind, p.mutation, &c)
	if err != nil {
		return Statement{}, err
	}
	prefix, err := c.compileCTEs(q.modelSelect())
	if err != nil {
		return Statement{}, err
	}
	sets := make([]string, 0, len(assigned))
	for _, assignment := range p.mutation.assignments {
		column := c.columns[assignment.field.column]
		parameter, err := c.assignmentParameter(column.Name, assignment.bind)
		if err != nil {
			return Statement{}, err
		}
		sets = append(sets, quoted(column.Name)+" = "+parameter)
	}
	var sql strings.Builder
	sql.WriteString(prefix)
	switch p.kind.sqlKind() {
	case updateModel:
		sql.WriteString("UPDATE " + quotedTable(q.table) + " SET " + strings.Join(sets, ", "))
	case deleteModel:
		sql.WriteString("DELETE FROM " + quotedTable(q.table))
	default:
		return Statement{}, fault.New(fault.Invalid, "invalid model mutation kind")
	}
	if err := c.where(&sql, q.effectivePredicates()); err != nil {
		return Statement{}, err
	}
	sql.WriteString(" RETURNING " + selectedColumns(q.table, q.definition.columns))
	return Statement{sql: sql.String(), arguments: c.arguments}, nil
}

func hasPrimaryEquality(predicates []expression, primary string) bool {
	for _, predicate := range predicates {
		switch predicate := predicate.(type) {
		case comparison:
			if field, ok := predicate.operand.(fieldRef); ok && field.column == primary && predicate.operator == equal {
				return true
			}
		case junction:
			if !predicate.any && hasPrimaryEquality(predicate.children, primary) {
				return true
			}
		}
	}
	return false
}
