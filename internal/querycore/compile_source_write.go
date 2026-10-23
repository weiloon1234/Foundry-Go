package querycore

import "strings"

// SourceWriteKey is lowered only after exact source/destination identity checks.
type SourceWriteKey struct {
	Expression ValueExpression
	Nullable   bool
}

// sourceWriteSelection keeps the caller's entire SELECT window below the match
// count. Adding the window function at the original level would incorrectly
// count rows that an explicit source LIMIT/OFFSET subsequently excludes.
func (q QueryCore) sourceWriteSelection(destination, source SelectNode, key SourceWriteKey, mapped map[string]ValueExpression) (SelectNode, *SelectNames, string, error) {
	source.Selections = []SelectItem{{Expression: key.Expression}}
	for _, column := range q.Model.Columns {
		if mapping, ok := mapped[column.Name]; ok {
			source.Selections = append(source.Selections, SelectItem{Expression: mapping, Alias: column.Name})
		}
	}
	names, err := NamesInSelects(destination, source)
	if err != nil {
		return SelectNode{}, nil, "", err
	}
	for _, column := range q.Model.Columns {
		names.Used[column.Name] = true
	}
	keyName := names.Allocate("foundry_source_key")
	source.Selections[0].Alias = keyName
	columns := []Column{{Name: keyName, Nullable: key.Nullable}}
	for _, column := range q.Model.Columns {
		if _, ok := mapped[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	definition := &CteNode{Name: names.Allocate("foundry_write_source"), Query: source, Columns: columns, Materialization: CteMaterialized}
	countName := ""
	if len(mapped) != 0 {
		countName = names.Allocate("foundry_source_matches")
		// Match using the destination's actual SQL key comparison. Partitioning
		// the source expression alone could miss duplicates when the source and
		// destination use different database comparison or collation semantics.
		matched := SelectNode{Source: TableSource{Table: q.Table, Columns: q.Model.Columns},
			Joins: []JoinNode{{Source: TableSource{Cte: definition, Columns: columns}, Kind: InnerJoin,
				On: BinaryComparison{Left: FieldRef{Table: q.Table, Column: q.Model.Primary}, Right: FieldRef{Table: definition.Name, Column: keyName}, Operator: Equal}}},
			Selections: []SelectItem{{Expression: FieldRef{Table: q.Table, Column: q.Model.Primary}, Alias: keyName}},
		}
		for _, column := range columns[1:] {
			matched.Selections = append(matched.Selections, SelectItem{Expression: FieldRef{Table: definition.Name, Column: column.Name}, Alias: column.Name})
		}
		matched.Selections = append(matched.Selections, SelectItem{Alias: countName, Expression: WindowNode{
			Kind: AggregateWindow, Aggregate: AggregateNode{Kind: CountAll},
			Window: WindowSpec{Partitions: []ValueExpression{FieldRef{Table: q.Table, Column: q.Model.Primary}}},
		}})
		columns = append(append([]Column(nil), columns...), Column{Name: countName})
		columns[0].Nullable = false
		definition = &CteNode{Name: names.Allocate("foundry_write_matches"), Query: matched, Columns: columns, Materialization: CteMaterialized}
	}

	joined := destination
	joined.Joins = []JoinNode{{Source: TableSource{Cte: definition, Columns: columns}, Kind: InnerJoin,
		On: BinaryComparison{Left: FieldRef{Table: q.Table, Column: q.Model.Primary}, Right: FieldRef{Table: definition.Name, Column: keyName}, Operator: Equal}}}
	return joined, names, countName, nil
}

// Source SELECTs, dependencies, predicates and all bindings use the shared AST
// compiler. Only the native mutation envelope is assembled here.
func (q QueryCore) CompileSourceWrite(c Compiler, destination, source SelectNode, key SourceWriteKey, kind MutationKind, mapped map[string]ValueExpression, values map[string]MutationAssignment, returning bool) (Statement, error) {
	c.BeginStatement(q.ScopeContext)
	node, names, countName, err := q.sourceWriteSelection(destination, source, key, mapped)
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
	sourceColumns := make(map[string]Column, len(join.Source.Columns))
	for _, column := range join.Source.Columns {
		sourceColumns[column.Name] = column
	}
	c.Sources[sourceName] = sourceColumns
	sets := make([]string, 0, len(mapped)+len(values))
	for _, column := range q.Model.Columns {
		var expression string
		if _, ok := mapped[column.Name]; ok {
			expression = Qualified(FieldRef{Table: sourceName, Column: column.Name})
		} else if assignment, ok := values[column.Name]; ok {
			expression, err = c.AssignmentParameter(column.Name, assignment.Bind)
			if err != nil {
				return Statement{}, err
			}
		} else {
			continue
		}
		sets = append(sets, Quoted(column.Name)+" = "+expression)
	}
	var body strings.Builder
	if kind.SqlKind() == UpdateModel {
		body.WriteString("UPDATE " + QuotedTable(q.Table) + " SET " + strings.Join(sets, ", ") + " FROM " + from)
	} else {
		body.WriteString("DELETE FROM " + QuotedTable(q.Table) + " USING " + from)
	}
	predicates := append(append([]Expression(nil), node.Predicates...), join.On)
	if err := c.Where(&body, predicates); err != nil {
		return Statement{}, err
	}
	if returning {
		body.WriteString(" RETURNING " + SelectedColumns(q.Table, q.Model.Columns))
		if countName != "" {
			body.WriteString(", " + Qualified(FieldRef{Table: sourceName, Column: countName}))
		}
		return c.Statement(prefix + body.String()), nil
	}
	if countName == "" {
		return c.Statement(prefix + body.String()), nil
	}
	// Checking two scalars inside the owning transaction avoids materializing
	// affected models for Exec. Every ambiguous group carries the same count,
	// regardless of which match PostgreSQL chooses before we reject and roll back.
	changes := names.Allocate("foundry_source_changes")
	sql := strings.TrimSuffix(prefix, " ") + ", " + Quoted(changes) + " AS (" + body.String() + " RETURNING " + Qualified(FieldRef{Table: sourceName, Column: countName}) + " AS " + Quoted(countName) + ") SELECT COUNT(*), COALESCE(MAX(" + Quoted(countName) + "), 0) FROM " + Quoted(changes)
	return c.Statement(sql), nil
}
