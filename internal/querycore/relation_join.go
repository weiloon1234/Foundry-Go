package querycore

import "github.com/weiloon1234/Foundry-Go/internal/sqlname"
import "github.com/weiloon1234/Foundry-Go/fault"

const RelationTargetAlias = "foundry_target"
const RelationPivotAlias = "foundry_pivot"
const RelationHopAlias = "foundry_through"

// RelationOrder preserves the combined target/pivot order without model types.
type RelationOrder struct {
	Value             ValueExpression
	Descending, Pivot bool
	Nulls             NullPlacement
}

// RelationThroughSelect is the shared joined-model SELECT owner for both APIs.
func RelationThroughSelect(target, pivot QueryCore, foreign, pivotForeign FieldRef, orders []RelationOrder, targetAlias, pivotAlias string) SelectNode {
	node := SelectNode{Source: TableSource{Table: target.Table, Alias: targetAlias, Columns: target.Model.Columns},
		Selections: append(ColumnSelections(targetAlias, target.Model.Columns), ColumnSelections(pivotAlias, pivot.Model.Columns)...),
		Joins:      []JoinNode{{Source: TableSource{Table: pivot.Table, Alias: pivotAlias, Columns: pivot.Model.Columns}, On: BinaryComparison{Left: FieldRef{Table: targetAlias, Column: foreign.Column}, Right: FieldRef{Table: pivotAlias, Column: pivotForeign.Column}, Operator: Equal}}}}
	for _, p := range target.EffectivePredicates() {
		node.Predicates = append(node.Predicates, Requalify(p, targetAlias))
	}
	for _, p := range pivot.EffectivePredicates() {
		node.Predicates = append(node.Predicates, Requalify(p, pivotAlias))
	}
	seen := make(map[FieldRef]bool, len(orders))
	for _, o := range orders {
		alias := targetAlias
		if o.Pivot {
			alias = pivotAlias
		}
		expr := RequalifyValue(o.Value, alias)
		node.Orders = append(node.Orders, OrderNode{Expression: expr, Descending: o.Descending, Nulls: o.Nulls})
		if f, ok := expr.(FieldRef); ok {
			seen[f] = true
		}
	}
	for _, f := range []FieldRef{{Table: targetAlias, Column: target.Model.Primary}, {Table: pivotAlias, Column: pivot.Model.Primary}} {
		if !seen[f] {
			node.Orders = append(node.Orders, OrderNode{Expression: f})
		}
	}
	return node
}

// RelationHopJoin adds the intermediate model with its own scopes.
func RelationHopJoin(node SelectNode, targetAlias string, foreign FieldRef, table string, columns []Column, predicates []Expression, second FieldRef) SelectNode {
	node.Joins = append(node.Joins, JoinNode{Source: TableSource{Table: table, Alias: RelationHopAlias, Columns: columns}, On: BinaryComparison{Left: FieldRef{Table: targetAlias, Column: foreign.Column}, Right: FieldRef{Table: RelationHopAlias, Column: second.Column}, Operator: Equal}})
	for _, p := range predicates {
		node.Predicates = append(node.Predicates, Requalify(p, RelationHopAlias))
	}
	return node
}

// ValidateMorphName uses the persisted identifier grammar for discriminators.
func ValidateMorphName(name string) error {
	if !sqlname.Valid(name) {
		return fault.New(fault.Invalid, "morph names use the identifier grammar")
	}
	return nil
}
