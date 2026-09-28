package datatable

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Normalize validates serialized client metadata against its actual row graph.
// It reuses scalar/filter agreement and sorting rules without constructing SQL
// expressions, invoking authorization or claiming an executable table binding.
func (d Description) Normalize() (Description, error) {
	if !identifier.Semantic(string(d.ID)) || len(d.Columns) == 0 || len(d.Columns) > MaxColumns || len(d.Filters) > MaxColumns {
		return Description{}, invalid("invalid datatable metadata")
	}
	var err error
	d.Row, err = d.Row.Normalize()
	if err != nil {
		return Description{}, err
	}
	d.Request, err = d.Request.Normalize()
	if err != nil {
		return Description{}, err
	}
	expected, err := RequestJSON().Description()
	if err != nil {
		return Description{}, err
	}
	if d.Request.Root != expected.Root {
		return Description{}, invalid("datatable request identity changed")
	}
	requestTypes := make(map[contract.TypeID]contract.Type)
	for _, typ := range d.Request.Types {
		requestTypes[typ.ID] = typ
	}
	for _, typ := range expected.Types {
		if !reflect.DeepEqual(requestTypes[typ.ID], typ) {
			return Description{}, invalid("datatable request shape changed")
		}
	}
	names := make([]string, 0, len(d.Columns))
	for _, column := range d.Columns {
		names = append(names, column.Name)
	}
	properties, err := d.Row.ScalarProperties(names...)
	if err != nil {
		return Description{}, err
	}
	d.Columns = slices.Clone(d.Columns)
	seen, sortable := make(map[string]bool), make(map[string]bool)
	exports := 0
	for i, column := range d.Columns {
		scalar, err := (contract.Schema{Root: column.Value.Value.ID, Types: []contract.Type{column.Value.Value}}).Normalize()
		if err != nil {
			return Description{}, err
		}
		column.Value.Value = scalar.Types[0]
		if !validName(column.Name) || seen[column.Name] || column.Label.Validate() != nil || !reflect.DeepEqual(column.Value, properties[i]) {
			return Description{}, invalid("column metadata differs from its DTO")
		}
		seen[column.Name], sortable[column.Name] = true, column.Sortable
		column.Value = properties[i]
		if column.Filter != nil {
			filter, err := normalizeFilterMetadata(*column.Filter)
			if err != nil {
				return Description{}, err
			}
			if !sameScalar(column.Value.Value, filter) {
				return Description{}, invalid("column filter differs from its DTO")
			}
			column.Filter = &filter
		}
		if column.Searchable {
			if column.Filter == nil || searchOperator(*column.Filter) == "" || column.SearchOperator != searchOperator(*column.Filter) {
				return Description{}, invalid("invalid searchable column")
			}
		} else if column.SearchOperator != "" {
			return Description{}, invalid("non-searchable column has a search operator")
		}
		if column.Exportable {
			exports++
		}
		d.Columns[i] = column
	}
	d.Filters = slices.Clone(d.Filters)
	for i, filter := range d.Filters {
		if !validName(filter.Name) || seen[filter.Name] || filter.Label.Validate() != nil {
			return Description{}, invalid("invalid extra filter metadata")
		}
		seen[filter.Name] = true
		filter.Filter, err = normalizeFilterMetadata(filter.Filter)
		if err != nil {
			return Description{}, err
		}
		d.Filters[i] = filter
	}
	if d.Exports && exports == 0 {
		return Description{}, invalid("table has no exportable columns")
	}
	d.DefaultSort = slices.Clone(d.DefaultSort)
	if err := validateSortColumns(d.DefaultSort, func(name string) bool { return sortable[name] }); err != nil {
		return Description{}, err
	}
	return d, nil
}

func normalizeFilterMetadata(info FilterInfo) (FilterInfo, error) {
	if info.Phase != WherePhase && info.Phase != HavingPhase || len(info.Operators) == 0 || len(info.Operators) > len((Operator("")).EnumDescriptor().Cases()) {
		return FilterInfo{}, invalid("invalid filter metadata")
	}
	result, err := cloneFilterInfo(info)
	if err != nil {
		return FilterInfo{}, err
	}
	seen := make(map[Operator]bool)
	for _, operator := range result.Operators {
		if !(Operator("")).EnumDescriptor().Contains(operator) || operator == All || operator == Any || operator == Not || seen[operator] || (operator == IsNull || operator == IsNotNull) && !result.Nullable {
			return FilterInfo{}, invalid("invalid filter operator metadata")
		}
		seen[operator] = true
	}
	slices.Sort(result.Operators)
	return result, nil
}
