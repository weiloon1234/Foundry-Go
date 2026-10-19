package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p sourceMutation[S, M]) validateShape(allowManaged bool) (compiler, map[string]UpdateMapping[S, M], map[string]mutationAssignment, error) {
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
	if err := p.key.field.Validate(q.queryCore.Table); err != nil {
		return c, nil, nil, err
	}
	primary, declared := q.definition.modelField(q.definition.modelDefinition.Primary)
	if !declared || p.key.field.Column != q.definition.modelDefinition.Primary || p.key.typ != primary.typ || p.key.expression == nil {
		return c, nil, nil, fault.New(fault.Invalid, "source write requires a compatible primary-key match")
	}
	values, err := q.validateMutationShape(p.kind, p.values, &c)
	if err != nil {
		return c, nil, nil, err
	}
	if len(p.mappings) > len(c.Columns) || (p.kind != updateModel && len(p.mappings) != 0) {
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
		if name == q.definition.modelDefinition.Primary || repeat || literal {
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
	if p.kind.SqlKind() == updateModel && len(mapped)+len(values) == 0 && !managed {
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
	source.Selections = []selectItem{{Expression: p.key.expression}}
	for _, column := range q.definition.modelDefinition.Columns {
		if mapping, ok := mapped[column.Name]; ok {
			source.Selections = append(source.Selections, selectItem{Expression: mapping.expression, Alias: column.Name})
		}
	}
	names, err := namesInSelects(q.modelSelect(), source)
	if err != nil {
		return selectNode{}, nil, "", err
	}
	for _, column := range q.definition.modelDefinition.Columns {
		names.Used[column.Name] = true
	}
	keyName := names.Allocate("foundry_source_key")
	source.Selections[0].Alias = keyName
	columns := []columnData{{Name: keyName, Nullable: p.key.nullable}}
	for _, column := range q.definition.modelDefinition.Columns {
		if _, ok := mapped[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	definition := &cteNode{Name: names.Allocate("foundry_write_source"), Query: source, Columns: columns, Materialization: cteMaterialized}
	countName := ""
	if len(mapped) != 0 {
		countName = names.Allocate("foundry_source_matches")
		// Match using the destination's actual SQL key comparison. Partitioning
		// the source expression alone could miss duplicates when the source and
		// destination use different database comparison or collation semantics.
		matched := selectNode{Source: tableSource{Table: q.queryCore.Table, Columns: q.definition.modelDefinition.Columns},
			Joins: []joinNode{{Source: tableSource{Cte: definition, Columns: columns}, Kind: innerJoin,
				On: binaryComparison{Left: fieldRef{Table: q.queryCore.Table, Column: q.definition.modelDefinition.Primary}, Right: fieldRef{Table: definition.Name, Column: keyName}, Operator: equal}}},
			Selections: []selectItem{{Expression: fieldRef{Table: q.queryCore.Table, Column: q.definition.modelDefinition.Primary}, Alias: keyName}},
		}
		for _, column := range columns[1:] {
			matched.Selections = append(matched.Selections, selectItem{Expression: fieldRef{Table: definition.Name, Column: column.Name}, Alias: column.Name})
		}
		matched.Selections = append(matched.Selections, selectItem{Alias: countName, Expression: windowNode{
			Kind: aggregateWindow, Aggregate: aggregateNode{Kind: countAll},
			Window: windowSpec{Partitions: []valueExpression{fieldRef{Table: q.queryCore.Table, Column: q.definition.modelDefinition.Primary}}},
		}})
		columns = append(append([]columnData(nil), columns...), columnData{Name: countName})
		columns[0].Nullable = false
		definition = &cteNode{Name: names.Allocate("foundry_write_matches"), Query: matched, Columns: columns, Materialization: cteMaterialized}
	}

	joined := q.modelSelect()
	joined.Joins = []joinNode{{Source: tableSource{Cte: definition, Columns: columns}, Kind: innerJoin,
		On: binaryComparison{Left: fieldRef{Table: q.queryCore.Table, Column: q.definition.modelDefinition.Primary}, Right: fieldRef{Table: definition.Name, Column: keyName}, Operator: equal}}}
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
	prefix, err := c.CompileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	join := node.Joins[0]
	from, err := c.SourceSQL(join.Source)
	if err != nil {
		return Statement{}, err
	}
	sourceName := join.Source.Name()
	sourceColumns := make(map[string]columnData, len(join.Source.Columns))
	for _, column := range join.Source.Columns {
		sourceColumns[column.Name] = column
	}
	c.Sources[sourceName] = sourceColumns
	q := p.destination
	sets := make([]string, 0, len(mapped)+len(values))
	for _, column := range q.definition.modelDefinition.Columns {
		var expression string
		if _, ok := mapped[column.Name]; ok {
			expression = qualified(fieldRef{Table: sourceName, Column: column.Name})
		} else if assignment, ok := values[column.Name]; ok {
			expression, err = c.AssignmentParameter(column.Name, assignment.Bind)
			if err != nil {
				return Statement{}, err
			}
		} else {
			continue
		}
		sets = append(sets, quoted(column.Name)+" = "+expression)
	}
	var body strings.Builder
	if p.kind.SqlKind() == updateModel {
		body.WriteString("UPDATE " + quotedTable(q.queryCore.Table) + " SET " + strings.Join(sets, ", ") + " FROM " + from)
	} else {
		body.WriteString("DELETE FROM " + quotedTable(q.queryCore.Table) + " USING " + from)
	}
	predicates := append(append([]expression(nil), node.Predicates...), join.On)
	if err := c.Where(&body, predicates); err != nil {
		return Statement{}, err
	}
	if returning {
		body.WriteString(" RETURNING " + selectedColumns(q.queryCore.Table, q.definition.modelDefinition.Columns))
		if countName != "" {
			body.WriteString(", " + qualified(fieldRef{Table: sourceName, Column: countName}))
		}
		return Statement{sql: prefix + body.String(), arguments: c.Arguments}, nil
	}
	if countName == "" {
		return Statement{sql: prefix + body.String(), arguments: c.Arguments}, nil
	}
	// Checking two scalars inside the owning transaction avoids materializing
	// affected models for Exec. Every ambiguous group carries the same count,
	// regardless of which match PostgreSQL chooses before we reject and roll back.
	changes := names.Allocate("foundry_source_changes")
	sql := strings.TrimSuffix(prefix, " ") + ", " + quoted(changes) + " AS (" + body.String() + " RETURNING " + qualified(fieldRef{Table: sourceName, Column: countName}) + " AS " + quoted(countName) + ") SELECT COUNT(*), COALESCE(MAX(" + quoted(countName) + "), 0) FROM " + quoted(changes)
	return Statement{sql: sql, arguments: c.Arguments}, nil
}
