package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func (p Conflict) ValidateShape(q QueryCore, c *Compiler, validateMutator func(ConflictUpdate) error) error {
	if p.Action != ConflictNothing && p.Action != ConflictDoUpdate {
		return safeerror.NewFault(fault.Invalid, "upsert requires an explicit conflict action")
	}
	if p.Named && (!sqlname.Valid(p.Constraint) || len(p.Keys) != 0) {
		return safeerror.NewFault(fault.Invalid, "invalid named conflict constraint")
	}
	if len(p.Keys) > MaxExpressionNodes || len(p.Updates) > len(c.Columns) {
		return safeerror.NewFault(fault.Invalid, "conflict columns exceed model metadata")
	}
	if err := p.validateTarget(q, c); err != nil {
		return err
	}
	if p.Action == ConflictNothing {
		if len(p.Updates) != 0 || len(p.Condition) != 0 || len(p.RowCondition) != 0 {
			return safeerror.NewFault(fault.Invalid, "DO NOTHING cannot contain update assignments or conditions")
		}
		return nil
	}
	if (!p.Named && len(p.Keys) == 0) || len(p.Updates) == 0 {
		return safeerror.NewFault(fault.Invalid, "conflict update requires a target and assignments")
	}
	seen := map[string]bool{}
	for _, u := range p.Updates {
		if err := u.Field.Validate(q.Table); err != nil {
			return err
		}
		_, declared := c.Columns[u.Field.Column]
		if !declared || seen[u.Field.Column] || u.Field.Column == q.Model.Primary || !u.validMode() {
			return safeerror.NewFault(fault.Invalid, "conflict update has an invalid, repeated or primary-key assignment")
		}
		if u.Null && !c.Columns[u.Field.Column].Nullable {
			return safeerror.NewFault(fault.Invalid, "NULL conflict assignment requires a nullable model field")
		}
		seen[u.Field.Column] = true
		if err := validateMutator(u); err != nil {
			return err
		}
	}
	return nil
}

// compile renders the conflict clause. scopes are the destination's active
// global scope predicates: DO UPDATE never changes a conflicting row outside
// them, so a tenant cannot overwrite another tenant's row through a globally
// unique key.
func (p Conflict) Compile(c *Compiler, table, alias string, columns []Column, scopes []Expression) (string, error) {
	var sql strings.Builder
	sql.WriteString(" ON CONFLICT")
	target, err := p.targetSQL(c, table)
	if err != nil {
		return "", err
	}
	sql.WriteString(target)
	if p.Action == ConflictNothing {
		return sql.String() + " DO NOTHING", nil
	}
	rename := correlationRenamer{from: ConflictStoredTable, to: alias}
	sets, err := compileUpdateSet(columns, len(p.Updates), func(column Column) (string, bool, error) {
		for _, update := range p.Updates {
			if update.Field.Column == column.Name {
				expression, err := update.compileValue(c, &rename)
				return expression, true, err
			}
		}
		return "", false, nil
	})
	if err != nil {
		return "", err
	}
	sql.WriteString(" DO UPDATE SET " + sets)
	conditions := make([]Expression, len(p.Condition), len(p.Condition)+len(p.RowCondition))
	for i, predicate := range p.Condition {
		conditions[i] = Requalify(predicate, alias)
	}
	for _, predicate := range p.RowCondition {
		conditions = append(conditions, rename.expression(predicate, 0))
	}
	for _, predicate := range scopes {
		conditions = append(conditions, Requalify(predicate, alias))
	}
	if rename.err != nil {
		return "", rename.err
	}
	if err := c.Where(&sql, conditions); err != nil {
		return "", err
	}
	return sql.String(), nil
}
