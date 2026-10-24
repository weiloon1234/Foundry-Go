package querycore

// Source is the single persisted-model source constructor, including relation
// inputs that do not select/hydrate the model itself.
func (d ModelDefinition) Source(table, alias string) TableSource {
	return TableSource{Table: table, Alias: alias, Columns: d.Columns, Sensitive: d.Sensitive}
}
