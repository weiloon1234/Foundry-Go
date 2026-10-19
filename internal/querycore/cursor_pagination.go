package querycore

// nullableOrder reports whether a model order key may be NULL. Computed keys
// and undeclared columns are treated as nullable.
func (d *ModelDefinition) NullableOrder(order QueryOrder) bool {
	if d == nil || order.Computed != nil {
		return true
	}
	for _, column := range d.Columns {
		if column.Name == order.Field.Column {
			return column.Nullable
		}
	}
	return true
}
