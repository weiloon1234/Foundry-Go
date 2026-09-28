package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxInsertRows bounds one atomic insert/upsert statement. The shared parameter
// bound additionally limits its row/column cells and actual bound values.
const MaxInsertRows = 1000

type insertPlan[M any] struct {
	query    Query[M]
	rows     []Mutation[M]
	conflict *Conflict[M]
}

func (p insertPlan[M]) compile() (Statement, error) {
	c, rows, union, err := p.validateRows()
	if err != nil {
		return Statement{}, err
	}
	return p.compileRows(c, rows, union)
}

// validateRows checks shape and resource bounds without binding input values.
// Mutation preparation reuses this before invoking any field transformation.
func (p insertPlan[M]) validateRows() (compiler, []map[string]Assignment[M], map[string]bool, error) {
	return p.validateRowShapes(true)
}

// validateRowShapes optionally requires all non-null fields after model conventions.
func (p insertPlan[M]) validateRowShapes(requireFields bool) (compiler, []map[string]Assignment[M], map[string]bool, error) {
	q := p.query
	c, err := q.mutationCompiler(insertModel)
	if err != nil {
		return c, nil, nil, err
	}
	if len(p.rows) > MaxInsertRows {
		return c, nil, nil, fault.New(fault.Invalid, "model insert exceeds its row bound")
	}
	if p.conflict != nil {
		if err := p.conflict.validate(q, &c); err != nil {
			return c, nil, nil, err
		}
	}
	rows := make([]map[string]Assignment[M], len(p.rows))
	union := make(map[string]bool)
	assignedCount := 0
	for i, row := range p.rows {
		if len(row.assignments) > MaxParameters-assignedCount {
			return c, nil, nil, fault.New(fault.Invalid, "model insert exceeds its cell bound")
		}
		assignedCount += len(row.assignments)
		if requireFields {
			rows[i], err = q.validateMutation(insertModel, row, &c)
		} else {
			rows[i], err = q.validateMutationShape(insertModel, row, &c)
		}
		if err != nil {
			return c, nil, nil, err
		}
		for name := range rows[i] {
			union[name] = true
		}
	}
	// PostgreSQL has no repeated DEFAULT VALUES form. An omitted primary column
	// is necessarily database-owned here; DEFAULT per row preserves that source.
	if len(union) == 0 && len(rows) > 1 {
		union[q.definition.primary] = true
	}
	if len(rows) > 0 && len(union) > MaxParameters/len(rows) {
		return c, nil, nil, fault.New(fault.Invalid, "model insert exceeds its cell bound")
	}
	return c, rows, union, nil
}

func (p insertPlan[M]) compileRows(c compiler, rows []map[string]Assignment[M], union map[string]bool) (Statement, error) {
	q := p.query
	node := q.modelSelect()
	if p.conflict != nil {
		node = p.conflict.dependencies(node)
	}
	alias := q.table
	if p.conflict != nil {
		names, err := namesInSelects(node)
		if err != nil {
			return Statement{}, err
		}
		alias = names.allocate("foundry_upsert")
	}
	prefix, err := c.compileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	var columns, quotedNames []string
	for _, col := range q.definition.columns {
		if union[col.Name] {
			columns = append(columns, col.Name)
			quotedNames = append(quotedNames, quoted(col.Name))
		}
	}
	values := make([]string, len(rows))
	for i, row := range rows {
		parts := make([]string, len(columns))
		for j, name := range columns {
			parts[j] = "DEFAULT"
			if a, present := row[name]; present {
				parts[j], err = c.assignmentParameter(name, a.bind)
				if err != nil {
					return Statement{}, err
				}
			}
		}
		values[i] = "(" + strings.Join(parts, ", ") + ")"
	}
	c.sources = map[string]map[string]Column{alias: c.columns}
	conflictSQL := ""
	if p.conflict != nil {
		c.sources[conflictProposedTable] = c.columns
		conflictSQL, err = p.conflict.compile(&c, q.table, alias)
		if err != nil {
			return Statement{}, err
		}
	}
	// Empty batches are still fully validated, but have no executable statement.
	if len(rows) == 0 {
		return Statement{}, nil
	}
	var sql strings.Builder
	sql.WriteString(prefix + "INSERT INTO " + quotedTable(q.table))
	if p.conflict != nil {
		sql.WriteString(" AS " + quoted(alias))
	}
	if len(columns) == 0 {
		sql.WriteString(" DEFAULT VALUES")
	} else {
		sql.WriteString(" (" + strings.Join(quotedNames, ", ") + ") VALUES " + strings.Join(values, ", "))
	}
	sql.WriteString(conflictSQL + " RETURNING " + selectedColumns(alias, q.definition.columns))
	return Statement{sql: sql.String(), arguments: c.arguments}, nil
}
