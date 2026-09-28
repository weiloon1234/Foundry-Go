package query

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p InsertSelect[S, M]) validateShape(allowManaged bool) (compiler, map[string]InsertMapping[S, M], map[string]Assignment[M], error) {
	q := p.destination
	c, err := q.mutationCompiler(insertModel)
	if err != nil {
		return c, nil, nil, err
	}
	if p.source.err != nil {
		return c, nil, nil, p.source.err
	}
	if len(p.mappings) > len(c.columns) {
		return c, nil, nil, fault.New(fault.Invalid, "insert mapping exceeds its field bound")
	}
	values, err := q.validateMutationShape(insertModel, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	mapped := make(map[string]InsertMapping[S, M], len(p.mappings))
	for _, mapping := range p.mappings {
		name, err := q.validateSQLMapping(mapping.modelValueMapping)
		if err != nil {
			return c, nil, nil, err
		}
		_, repeat := mapped[name]
		_, literal := values[name]
		if repeat || literal {
			return c, nil, nil, fault.New(fault.Invalid, "insert mapping repeats an assigned field")
		}
		mapped[name] = mapping
	}
	for name := range values {
		if _, declared := q.definition.modelField(name); !declared {
			return c, nil, nil, fault.New(fault.Invalid, "insert values require declared field codec metadata")
		}
	}
	err = q.requireInsertFields(func(name string) bool {
		_, mapping := mapped[name]
		_, literal := values[name]
		managed := allowManaged && q.hasTimestamps() && (name == q.definition.timestamps.created || name == q.definition.timestamps.updated)
		return mapping || literal || managed
	})
	if err != nil {
		return c, nil, nil, err
	}
	if len(mapped)+len(values) == 0 && !(allowManaged && q.hasTimestamps()) {
		return c, nil, nil, fault.New(fault.Invalid, "insert from query requires at least one supplied column")
	}
	return c, mapped, values, nil
}

func (p InsertSelect[S, M]) prepare(returning bool) func(context.Context, *database.Tx) (Statement, error) {
	return func(ctx context.Context, tx *database.Tx) (Statement, error) {
		prepared, err := p.prepareValues(ctx, transactionClock(tx))
		if err != nil {
			return Statement{}, err
		}
		return prepared.compile(returning)
	}
}

func (p InsertSelect[S, M]) prepareValues(ctx context.Context, source clock.Clock) (InsertSelect[S, M], error) {
	if ctx == nil {
		return p, fault.New(fault.Invalid, "insert preparation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	_, mapped, values, err := p.validateShape(true)
	if err != nil {
		return p, err
	}
	q := p.destination
	if q.hasTimestamps() {
		now, err := modelTimestamp(ctx, source)
		if err != nil {
			return p, err
		}
		p.values, err = q.applyTimestampsPresent(insertModel, p.values, now, func(name string) bool {
			_, mapping := mapped[name]
			_, literal := values[name]
			return mapping || literal
		})
		if err != nil {
			return p, err
		}
	}
	if q.hasFieldMutators() {
		p.values, err = q.mutateFields(ctx, p.values)
		if err != nil {
			return p, err
		}
	}
	return p, ctx.Err()
}

// The existing SELECT compiler owns source scopes, CTE dependencies, expression
// validation and bindings. Destination declaration order owns insertion columns.
// This private compiler accepts only already-prepared literal values.
func (p InsertSelect[S, M]) compile(returning bool) (Statement, error) {
	c, mapped, values, err := p.validateShape(false)
	if err != nil {
		return Statement{}, err
	}
	q := p.destination
	node := p.source.node
	node.selections = make([]selectItem, 0, len(mapped)+len(values))
	columns := make([]string, 0, cap(node.selections))
	for _, column := range q.definition.columns {
		var expression valueExpression
		if mapping, ok := mapped[column.Name]; ok {
			expression = mapping.expression
		} else if assignment, ok := values[column.Name]; ok {
			bound, err := bindAssignment(column, assignment.bind)
			if err != nil {
				return Statement{}, err
			}
			field, _ := q.definition.modelField(column.Name)
			expression = parameterNode{kind: field.kind, value: bound}
		} else {
			continue
		}
		columns = append(columns, quoted(column.Name))
		node.selections = append(node.selections, selectItem{expression: expression, alias: column.Name})
	}
	prefix, err := c.compileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	selected, err := c.selectSQL(node)
	if err != nil {
		return Statement{}, err
	}
	sql := prefix + "INSERT INTO " + quotedTable(q.table) + " (" + strings.Join(columns, ", ") + ") " + selected
	if returning {
		sql += " RETURNING " + selectedColumns(q.table, q.definition.columns)
	}
	return Statement{sql: sql, arguments: c.arguments}, nil
}
