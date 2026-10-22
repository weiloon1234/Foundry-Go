package goway_test

func compilerCases() []compilerCase {
	return []compilerCase{
		{"wrong_predicate", "orm.Predicate[models.Order]"},
		{"wrong_eq", "as int64 value"},
		{"enum_ordered", "has no field or method Gt"},
		{"required_null_set", "has no field or method SetNull"},
		{"required_null_test", "has no field or method IsNull"},
		{"number_text", "has no field or method Contains"},
		{"locked_pool", "*database.Tx"},
		{"wrong_key", "model.ID[models.Order]"},
		{"relation_key", "does not match inferred type"},
		{"wrong_relation", "orm.Relation[models.Order]"},
		{"text_adjustment", "has no field or method Increment"},
		{"wrong_order", "orm.Order[models.Order]"},
		{"wrong_assignment", "orm.Assignment[models.Order]"},
		{"number_key", "does not satisfy"},
		{"int_text_key", "does not satisfy"},
		{"projection_type", "does not match inferred type"},
		{"join_scope", "does not match inferred type"},
		{"select_owner", "does not match inferred type"},
		{"nullable_eq_null", "as model.ID[models.User] value"},
		{"joined_right", "does not match rows.JoinTable[R]"},
	}
}
