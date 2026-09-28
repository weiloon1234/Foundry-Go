package generate

func (e *emitter) emitModelFields(m model) {
	query := e.use(framework + "/database/query")
	e.line("// %sFieldSet exposes fields owned by the unaliased model.", m.name)
	e.line("type %sFieldSet = %sScopedFieldSet[%s]", m.name, m.name, m.name)
	e.line("// %sFields returns independent immutable field descriptors.", m.name)
	e.line("func %sFields()%sFieldSet{return %sFieldsAt(%s.DeclareModelScope[%s](%q))}", m.name, m.name, m.name, query, m.name, m.table)
	e.emitScopedFields(m.name, m.fields, m.table, "ModelScope", "NullableModelScope")
}

// Models and declared result records share field capability and codec emission.
func (e *emitter) emitScopedFields(name string, fields []field, source, ordinaryScope, nullableScope string) {
	query := e.use(framework + "/database/query")
	for _, f := range fields {
		_ = e.typeName(f.base)
		_ = e.fieldCodec(f, false)
		_ = e.fieldInputType(f, false)
	}
	for _, f := range fields {
		if f.kind == "JSON" {
			e.emitJSONField(name, f)
		}
		if f.input != nil {
			e.emitInputField(name, source, f)
		}
	}
	scope, scopeVar := e.localName("FoundryScope"), e.localName("scope")
	for _, nullable := range []bool{false, true} {
		setName, function, scopeType := name+"ScopedFieldSet", name+"FieldsAt", ordinaryScope
		if nullable {
			setName, function, scopeType = name+"NullableFieldSet", name+"NullableFieldsAt", nullableScope
		}
		e.line("// %s preserves typed field operators in a declared query scope.", setName)
		e.line("type %s[%s any] struct{", setName, scope)
		for _, original := range fields {
			f := original
			f.nullable = f.nullable || nullable
			e.line("// %s describes %s.%s.", f.name, source, f.column)
			e.emitFieldBehavior(name, source, original)
			_, typ := e.baseFieldType(name, f, scope)
			if f.input != nil {
				typ = inputFieldName(name, f) + "[" + scope + "]"
			}
			e.line("%s %s", f.name, typ)
		}
		e.line("}")
		e.line("// %s binds these record fields to an alias or joined scope.", function)
		e.line("func %s[%s any](%s %s.%s[%s,%s])%s[%s]{return %s[%s]{", function, scope, scopeVar, query, scopeType, scope, name, setName, scope, setName, scope)
		for _, f := range fields {
			f.nullable = f.nullable || nullable
			table := scopeVar + ".Table()"
			base := e.baseFieldValue(name, f, scope, table)
			if f.input != nil {
				fieldName, _ := e.baseFieldType(name, f, scope)
				if c := e.binaryInputCodec(f, false); c != "" {
					e.line("%s:%s[%s]{%s:%s,input:%s.NewClonedMutationInputField[%s,%s](%s,%q,%s)},", f.name, inputFieldName(name, f), scope, fieldName, base, query, scope, e.fieldInputType(f, false), table, f.column, c)
				} else {
					e.line("%s:%s[%s]{%s:%s,input:%s.NewMutationInputField[%s,%s](%s,%q)},", f.name, inputFieldName(name, f), scope, fieldName, base, query, scope, e.fieldInputType(f, false), table, f.column)
				}
			} else {
				e.line("%s:%s,", f.name, base)
			}
		}
		e.line("}}")
	}
}

func (e *emitter) emitFieldBehavior(owner, table string, f field) {
	for _, note := range fieldBehaviorNotes(owner, table, f) {
		e.line("// %s", note)
	}
}
