package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p sourceMutation[S, M]) validateShape(allowManaged bool) (compiler, map[string]UpdateMapping[S, M], map[string]Assignment[M], error) {
	q := p.destination
	if p.source.err != nil {
		return compiler{}, nil, nil, p.source.err
	}
	c, err := q.modelWriteCompiler(p.kind, false)
	if err != nil {
		return c, nil, nil, err
	}
	if p.kind != updateModel && p.kind != deleteModel && p.kind != softDeleteModel && p.kind != forceDeleteModel {
		return c, nil, nil, fault.New(fault.Invalid, "invalid source write kind")
	}
	if err := p.key.field.validate(q.table); err != nil {
		return c, nil, nil, err
	}
	primary, declared := q.definition.modelField(q.definition.primary)
	if !declared || p.key.field.column != q.definition.primary || p.key.typ != primary.typ || p.key.expression == nil {
		return c, nil, nil, fault.New(fault.Invalid, "source write requires a compatible primary-key match")
	}
	values, err := q.validateMutationShape(p.kind, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	if len(p.mappings) > len(c.columns) || (p.kind != updateModel && len(p.mappings) != 0) {
		return c, nil, nil, fault.New(fault.Invalid, "source write has invalid or excessive mappings")
	}
	mapped := make(map[string]UpdateMapping[S, M], len(p.mappings))
	for _, mapping := range p.mappings {
		name, err := q.validateSQLMapping(mapping.modelValueMapping)
		if err != nil {
			return c, nil, nil, err
		}
		_, repeat := mapped[name]
		_, literal := values[name]
		if name == q.definition.primary || repeat || literal {
			return c, nil, nil, fault.New(fault.Invalid, "source update repeats an assignment or changes a primary key")
		}
		mapped[name] = mapping
	}
	for name := range values {
		if _, declared := q.definition.modelField(name); !declared {
			return c, nil, nil, fault.New(fault.Invalid, "source write values require declared field codec metadata")
		}
	}
	managed := allowManaged && (mutationPlan[M]{query: q, kind: p.kind}).needsConventions()
	if p.kind.sqlKind() == updateModel && len(mapped)+len(values) == 0 && !managed {
		return c, nil, nil, fault.New(fault.Invalid, "source update requires at least one assigned field")
	}
	return c, mapped, values, nil
}

// sourceWriteSelection keeps the caller's entire SELECT window below the match
// count. Adding the window function at the original level would incorrectly
// count rows that an explicit source LIMIT/OFFSET subsequently excludes.
func (p sourceMutation[S, M]) sourceWriteSelection(mapped map[string]UpdateMapping[S, M]) (selectNode, *selectNames, string, error) {
	q := p.destination
	source := p.source.node
	source.selections = []selectItem{{expression: p.key.expression}}
	for _, column := range q.definition.columns {
		if mapping, ok := mapped[column.Name]; ok {
			source.selections = append(source.selections, selectItem{expression: mapping.expression, alias: column.Name})
		}
	}
	names, err := namesInSelects(q.modelSelect(), source)
	if err != nil {
		return selectNode{}, nil, "", err
	}
	for _, column := range q.definition.columns {
		names.used[column.Name] = true
	}
	keyName := names.allocate("foundry_source_key")
	source.selections[0].alias = keyName
	columns := []Column{{Name: keyName, Nullable: p.key.nullable}}
	for _, column := range q.definition.columns {
		if _, ok := mapped[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	definition := &cteNode{name: names.allocate("foundry_write_source"), query: source, columns: columns, materialization: cteMaterialized}
	countName := ""
	if len(mapped) != 0 {
		countName = names.allocate("foundry_source_matches")
		// Match using the destination's actual SQL key comparison. Partitioning
		// the source expression alone could miss duplicates when the source and
		// destination use different database comparison or collation semantics.
		matched := selectNode{source: tableSource{table: q.table, columns: q.definition.columns},
			joins: []joinNode{{source: tableSource{cte: definition, columns: columns}, kind: innerJoin,
				on: binaryComparison{fieldRef{q.table, q.definition.primary}, fieldRef{definition.name, keyName}, equal}}},
			selections: []selectItem{{expression: fieldRef{q.table, q.definition.primary}, alias: keyName}},
		}
		for _, column := range columns[1:] {
			matched.selections = append(matched.selections, selectItem{expression: fieldRef{definition.name, column.Name}, alias: column.Name})
		}
		matched.selections = append(matched.selections, selectItem{alias: countName, expression: windowNode{
			kind: aggregateWindow, aggregate: aggregateNode{kind: countAll},
			window: windowSpec{partitions: []valueExpression{fieldRef{q.table, q.definition.primary}}},
		}})
		columns = append(append([]Column(nil), columns...), Column{Name: countName})
		columns[0].Nullable = false
		definition = &cteNode{name: names.allocate("foundry_write_matches"), query: matched, columns: columns, materialization: cteMaterialized}
	}

	joined := q.modelSelect()
	joined.joins = []joinNode{{source: tableSource{cte: definition, columns: columns}, kind: innerJoin,
		on: binaryComparison{fieldRef{q.table, q.definition.primary}, fieldRef{definition.name, keyName}, equal}}}
	return joined, names, countName, nil
}

// Source SELECTs, dependencies, predicates and all bindings use the shared AST
// compiler. Only the native mutation envelope is assembled here.
func (p sourceMutation[S, M]) compile(returning bool) (Statement, error) {
	c, mapped, values, err := p.validateShape(false)
	if err != nil {
		return Statement{}, err
	}
	node, names, countName, err := p.sourceWriteSelection(mapped)
	if err != nil {
		return Statement{}, err
	}
	prefix, err := c.compileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	join := node.joins[0]
	from, err := c.sourceSQL(join.source)
	if err != nil {
		return Statement{}, err
	}
	sourceName := join.source.name()
	sourceColumns := make(map[string]Column, len(join.source.columns))
	for _, column := range join.source.columns {
		sourceColumns[column.Name] = column
	}
	c.sources[sourceName] = sourceColumns
	q := p.destination
	sets := make([]string, 0, len(mapped)+len(values))
	for _, column := range q.definition.columns {
		var expression string
		if _, ok := mapped[column.Name]; ok {
			expression = qualified(fieldRef{sourceName, column.Name})
		} else if assignment, ok := values[column.Name]; ok {
			expression, err = c.assignmentParameter(column.Name, assignment.bind)
			if err != nil {
				return Statement{}, err
			}
		} else {
			continue
		}
		sets = append(sets, quoted(column.Name)+" = "+expression)
	}
	var body strings.Builder
	if p.kind.sqlKind() == updateModel {
		body.WriteString("UPDATE " + quotedTable(q.table) + " SET " + strings.Join(sets, ", ") + " FROM " + from)
	} else {
		body.WriteString("DELETE FROM " + quotedTable(q.table) + " USING " + from)
	}
	predicates := append(append([]expression(nil), node.predicates...), join.on)
	if err := c.where(&body, predicates); err != nil {
		return Statement{}, err
	}
	if returning {
		body.WriteString(" RETURNING " + selectedColumns(q.table, q.definition.columns))
		if countName != "" {
			body.WriteString(", " + qualified(fieldRef{sourceName, countName}))
		}
		return Statement{sql: prefix + body.String(), arguments: c.arguments}, nil
	}
	if countName == "" {
		return Statement{sql: prefix + body.String(), arguments: c.arguments}, nil
	}
	// Checking two scalars inside the owning transaction avoids materializing
	// affected models for Exec. Every ambiguous group carries the same count,
	// regardless of which match PostgreSQL chooses before we reject and roll back.
	changes := names.allocate("foundry_source_changes")
	sql := strings.TrimSuffix(prefix, " ") + ", " + quoted(changes) + " AS (" + body.String() + " RETURNING " + qualified(fieldRef{sourceName, countName}) + " AS " + quoted(countName) + ") SELECT COUNT(*), COALESCE(MAX(" + quoted(countName) + "), 0) FROM " + quoted(changes)
	return Statement{sql: sql, arguments: c.arguments}, nil
}
