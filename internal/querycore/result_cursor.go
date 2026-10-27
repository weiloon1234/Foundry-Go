package querycore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ResultCursorNode preserves the complete input window behind an allocated alias.
func ResultCursorNode(source SelectNode, columns []Column, hasDecoder bool) (SelectNode, error) {
	if len(columns) == 0 || len(columns) != len(source.Selections) || len(columns) > MaxExpressionNodes || !hasDecoder {
		return SelectNode{}, safeerror.NewFault(fault.Invalid, "cursor result requires bounded columns and a complete decoder")
	}
	names, err := NamesInSelects(source)
	if err != nil {
		return SelectNode{}, err
	}
	alias := names.Allocate("foundry_cursor")
	source.Selections = slices.Clone(source.Selections)
	for i, c := range columns {
		source.Selections[i].Alias = c.Name
	}
	return SelectNode{Source: TableSource{Query: &source, Alias: alias, Columns: slices.Clone(columns)}, Selections: ColumnSelections(alias, columns)}, nil
}

// ResultCursorOrders validates identity and appends missing identity fields.
func ResultCursorOrders(node SelectNode, columns []Column, orders []QueryOrder, unique []FieldRef) ([]QueryOrder, error) {
	fail := func() ([]QueryOrder, error) {
		return nil, safeerror.NewFault(fault.Invalid, "cursor result requires bounded, unique output fields and a UniqueBy declaration")
	}
	if node.Source.Query == nil || len(unique) == 0 || len(unique) > MaxCursorFields || len(orders) > MaxCursorFields {
		return fail()
	}
	names := make(map[string]bool, len(columns))
	for _, c := range columns {
		names[c.Name] = true
	}
	valid := func(f FieldRef) bool { return names[f.Column] && f.Table == node.Source.Name() }
	seen := make(map[string]bool, len(orders))
	for _, o := range orders {
		if !valid(o.Field) || seen[o.Field.Column] || o.Computed != nil {
			return fail()
		}
		seen[o.Field.Column] = true
	}
	orders = slices.Clone(orders)
	keys := make(map[string]bool, len(unique))
	for _, k := range unique {
		if !valid(k) || keys[k.Column] {
			return fail()
		}
		keys[k.Column] = true
		if !seen[k.Column] {
			orders = append(orders, QueryOrder{Field: k})
			seen[k.Column] = true
		}
	}
	if len(orders) > MaxCursorFields {
		return nil, safeerror.NewFault(fault.Invalid, "cursor result exceeds its field bound")
	}
	return orders, nil
}

// ResultCursorField describes codec availability without retaining typed getters.
type ResultCursorField struct {
	Name             string
	Sensitive, Valid bool
}

// ResultCursorFields shares output codec/identity validation across typed adapters.
func ResultCursorFields(node SelectNode, columns []Column, orders []QueryOrder, fields []ResultCursorField) ([]int, []bool, error) {
	if len(fields) > len(columns) {
		return nil, nil, safeerror.NewFault(fault.Invalid, "cursor result exceeds its field bound")
	}
	sensitivity, err := SelectSensitivity(node)
	if err != nil {
		return nil, nil, err
	}
	if len(sensitivity) != len(columns) {
		return nil, nil, safeerror.NewFault(fault.Invalid, "cursor sensitivity requires complete output metadata")
	}
	sensitive := make(map[string]bool, len(columns))
	byColumn := make(map[string]Column, len(columns))
	for i, c := range columns {
		sensitive[c.Name] = sensitivity[i]
		byColumn[c.Name] = c
	}
	getters := make(map[string]int, len(fields))
	for i, f := range fields {
		if _, ok := getters[f.Name]; ok || !f.Valid {
			return nil, nil, safeerror.NewFault(fault.Invalid, "cursor result has invalid field getters")
		}
		if _, ok := byColumn[f.Name]; !ok {
			return nil, nil, safeerror.NewFault(fault.Invalid, "cursor getter is not a declared output field")
		}
		getters[f.Name] = i
	}
	indices := make([]int, len(orders))
	required := make([]bool, len(orders))
	for i, o := range orders {
		j, ok := getters[o.Field.Column]
		if !ok {
			return nil, nil, safeerror.NewFault(fault.Invalid, "cursor result requires declared codecs and getters for every ordered field")
		}
		if fields[j].Sensitive || sensitive[o.Field.Column] {
			return nil, nil, safeerror.NewFault(fault.Invalid, "sensitive fields cannot be cursor keys")
		}
		indices[i] = j
		required[i] = !byColumn[o.Field.Column].Nullable
	}
	return indices, required, nil
}
