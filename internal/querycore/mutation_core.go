package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func (q QueryCore) ValidateMutationShape(kind MutationKind, assignments []MutationAssignment, c *Compiler) (map[string]MutationAssignment, error) {
	kind = kind.SqlKind()
	if len(assignments) > len(c.Columns) {
		return nil, fault.New(fault.Invalid, "mutation has more assignments than declared model fields")
	}
	assigned := make(map[string]MutationAssignment, len(assignments))
	for _, a := range assignments {
		if err := a.Field.Validate(q.Table); err != nil {
			return nil, err
		}
		_, declared := c.Columns[a.Field.Column]
		_, repeated := assigned[a.Field.Column]
		if !declared || repeated || a.Bind == nil {
			return nil, fault.New(fault.Invalid, "mutation has an undeclared, repeated or invalid assignment")
		}
		if kind != InsertModel && a.Field.Column == q.Model.Primary {
			return nil, fault.New(fault.Invalid, "model primary keys cannot be patched")
		}
		assigned[a.Field.Column] = a
	}
	if kind == DeleteModel && len(assigned) != 0 {
		return nil, fault.New(fault.Invalid, "model deletion cannot assign fields")
	}
	return assigned, nil
}

func (q QueryCore) ValidateMutation(kind MutationKind, assignments []MutationAssignment, c *Compiler) (map[string]MutationAssignment, error) {
	kind = kind.SqlKind()
	assigned, err := q.ValidateMutationShape(kind, assignments, c)
	if err != nil {
		return nil, err
	}
	if kind == InsertModel {
		if err := q.RequireInsertFields(func(name string) bool { _, set := assigned[name]; return set }); err != nil {
			return nil, err
		}
	}
	if kind == UpdateModel && len(assigned) == 0 {
		return nil, fault.New(fault.Invalid, "model update requires at least one assigned field")
	}
	return assigned, nil
}

func ReadMutationAssignments(assignments []MutationAssignment) (map[FieldRef]AssignmentValue, error) {
	if len(assignments) > MaxExpressionNodes {
		return nil, fault.New(fault.Invalid, "mutation exceeds its assignment bound")
	}
	values := make(map[FieldRef]AssignmentValue, len(assignments))
	for _, assignment := range assignments {
		if err := assignment.Field.Validate(assignment.Field.Table); err != nil {
			return nil, err
		}
		if _, duplicate := values[assignment.Field]; duplicate || assignment.Bind == nil {
			return nil, fault.New(fault.Invalid, "mutation has a repeated or invalid assignment")
		}
		values[assignment.Field] = assignment.AssignmentValue
	}
	return values, nil
}

func (q QueryCore) AdjustSet(a *Adjustment, c *Compiler, assigned map[string]MutationAssignment) (string, error) {
	if err := a.Field.Validate(q.Table); err != nil {
		return "", err
	}
	column, declared := c.Columns[a.Field.Column]
	if _, repeated := assigned[a.Field.Column]; !declared || repeated || a.Field.Column == q.Model.Primary || a.Bind == nil {
		return "", fault.New(fault.Invalid, "numeric adjustment requires a declared, unassigned non-key field")
	}
	bound, err := a.Bind()
	if err != nil {
		return "", err
	}
	if bound == nil {
		return "", fault.New(fault.Invalid, "numeric adjustment requires a non-NULL delta")
	}
	parameter, err := c.Parameter(bound)
	if err != nil {
		return "", err
	}
	op := " + "
	if a.Subtract {
		op = " - "
	}
	return Quoted(column.Name) + " = " + Quoted(column.Name) + op + parameter, nil
}

func (q QueryCore) CompileMutation(c Compiler, node SelectNode, kind MutationKind, assignments []MutationAssignment, assigned map[string]MutationAssignment, adjust *Adjustment, countOnly bool) (Statement, error) {
	prefix, err := c.CompileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	sets := make([]string, 0, len(assigned))
	for _, assignment := range assignments {
		column := c.Columns[assignment.Field.Column]
		parameter, err := c.AssignmentParameter(column.Name, assignment.Bind)
		if err != nil {
			return Statement{}, err
		}
		sets = append(sets, Quoted(column.Name)+" = "+parameter)
	}
	if adjust != nil {
		set, err := q.AdjustSet(adjust, &c, assigned)
		if err != nil {
			return Statement{}, err
		}
		sets = append(sets, set)
	}
	var sql strings.Builder
	sql.WriteString(prefix)
	switch kind.SqlKind() {
	case UpdateModel:
		sql.WriteString("UPDATE " + QuotedTable(q.Table) + " SET " + strings.Join(sets, ", "))
	case DeleteModel:
		sql.WriteString("DELETE FROM " + QuotedTable(q.Table))
	default:
		return Statement{}, fault.New(fault.Invalid, "invalid model mutation kind")
	}
	if err := c.Where(&sql, q.EffectivePredicates()); err != nil {
		return Statement{}, err
	}
	if !countOnly {
		sql.WriteString(" RETURNING " + SelectedColumns(q.Table, q.Model.Columns))
	}
	return Statement{SQLText: sql.String(), Bindings: c.Arguments}, nil
}

func (q QueryCore) CompileInsert(c Compiler, rows []map[string]MutationAssignment, union map[string]bool, node SelectNode, conflict *Conflict) (Statement, error) {
	alias := q.Table
	if conflict != nil {
		names, err := NamesInSelects(node)
		if err != nil {
			return Statement{}, err
		}
		alias = names.Allocate("foundry_upsert")
	}
	prefix, err := c.CompileCTEs(node)
	if err != nil {
		return Statement{}, err
	}
	var columns, quotedNames []string
	for _, col := range q.Model.Columns {
		if union[col.Name] {
			columns = append(columns, col.Name)
			quotedNames = append(quotedNames, Quoted(col.Name))
		}
	}
	values := make([]string, len(rows))
	for i, row := range rows {
		parts := make([]string, len(columns))
		for j, name := range columns {
			parts[j] = "DEFAULT"
			if a, present := row[name]; present {
				parts[j], err = c.AssignmentParameter(name, a.Bind)
				if err != nil {
					return Statement{}, err
				}
			}
		}
		values[i] = "(" + strings.Join(parts, ", ") + ")"
	}
	c.Sources = map[string]map[string]Column{alias: c.Columns}
	conflictSQL := ""
	if conflict != nil {
		c.Sources[ConflictProposedTable] = c.Columns
		conflictSQL, err = conflict.Compile(&c, q.Table, alias, q.ScopePredicates())
		if err != nil {
			return Statement{}, err
		}
	}
	// Empty batches are still fully validated, but have no executable statement.
	if len(rows) == 0 {
		return Statement{}, nil
	}
	var sql strings.Builder
	sql.WriteString(prefix + "INSERT INTO " + QuotedTable(q.Table))
	if conflict != nil {
		sql.WriteString(" AS " + Quoted(alias))
	}
	if len(columns) == 0 {
		sql.WriteString(" DEFAULT VALUES")
	} else {
		sql.WriteString(" (" + strings.Join(quotedNames, ", ") + ") VALUES " + strings.Join(values, ", "))
	}
	sql.WriteString(conflictSQL + " RETURNING " + SelectedColumns(alias, q.Model.Columns))
	return Statement{SQLText: sql.String(), Bindings: c.Arguments}, nil
}
