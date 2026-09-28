package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func (p Conflict[M]) validate(q Query[M], c *compiler) error {
	if p.action != conflictNothing && p.action != conflictUpdate {
		return fault.New(fault.Invalid, "upsert requires an explicit conflict action")
	}
	if p.named && (!sqlname.Valid(p.constraint) || len(p.keys) != 0) {
		return fault.New(fault.Invalid, "invalid named conflict constraint")
	}
	if len(p.keys) > MaxExpressionNodes || len(p.updates) > len(c.columns) {
		return fault.New(fault.Invalid, "conflict columns exceed model metadata")
	}
	if err := p.validateTarget(q, c); err != nil {
		return err
	}
	if p.action == conflictNothing {
		if len(p.updates) != 0 || len(p.condition) != 0 || len(p.rowCondition) != 0 {
			return fault.New(fault.Invalid, "DO NOTHING cannot contain update assignments or conditions")
		}
		return nil
	}
	if (!p.named && len(p.keys) == 0) || len(p.updates) == 0 {
		return fault.New(fault.Invalid, "conflict update requires a target and assignments")
	}
	seen := map[string]bool{}
	for _, u := range p.updates {
		if err := u.field.validate(q.table); err != nil {
			return err
		}
		_, declared := c.columns[u.field.column]
		if !declared || seen[u.field.column] || u.field.column == q.definition.primary || !u.validMode() {
			return fault.New(fault.Invalid, "conflict update has an invalid, repeated or primary-key assignment")
		}
		if u.null && !c.columns[u.field.column].Nullable {
			return fault.New(fault.Invalid, "NULL conflict assignment requires a nullable model field")
		}
		seen[u.field.column] = true
		if err := u.validateMutator(q); err != nil {
			return err
		}
	}
	// Reuse ordinary model expression validation and its bounds before walking
	// subqueries or requalifying references to the target row's allocated alias.
	q.predicates = p.condition
	if err := q.Validate(); err != nil {
		return err
	}
	return p.validateRows(c)
}

func (p Conflict[M]) compile(c *compiler, table, alias string) (string, error) {
	var sql strings.Builder
	sql.WriteString(" ON CONFLICT")
	target, err := p.targetSQL(c, table)
	if err != nil {
		return "", err
	}
	sql.WriteString(target)
	if p.action == conflictNothing {
		return sql.String() + " DO NOTHING", nil
	}
	sets := make([]string, len(p.updates))
	rename := correlationRenamer{from: conflictStoredTable, to: alias}
	for i, u := range p.updates {
		v, err := u.compileValue(c, &rename)
		if err != nil {
			return "", err
		}
		sets[i] = quoted(u.field.column) + " = " + v
	}
	sql.WriteString(" DO UPDATE SET " + strings.Join(sets, ", "))
	conditions := make([]expression, len(p.condition), len(p.condition)+len(p.rowCondition))
	for i, predicate := range p.condition {
		conditions[i] = requalify(predicate, alias)
	}
	for _, predicate := range p.rowCondition {
		conditions = append(conditions, rename.expression(predicate, 0))
	}
	if rename.err != nil {
		return "", rename.err
	}
	if err := c.where(&sql, conditions); err != nil {
		return "", err
	}
	return sql.String(), nil
}
