package generate

func emitProjection(p *packageInput, projection projection, dto *dtoDeclaration) ([]byte, error) {
	e := newEmitter(p)
	query, database := e.use(framework+"/database/query"), e.use(framework+"/database")
	name := projection.name
	types, codecs := make([]string, len(projection.fields)), make([]string, len(projection.fields))
	for i, f := range projection.fields {
		types[i] = e.typeName(f.typ)
		codecs[i] = e.fieldCodec(f, true)
	}
	scope := e.localName("FoundryScope")
	fieldsVar, rowVar, itemVar := e.localName("fields"), e.localName("row"), e.localName("item")
	e.line("// %sFieldSet exposes the declared result fields and their exact value types.", name)
	e.line("type %sFieldSet struct{", name)
	for i, f := range projection.fields {
		e.line("%s %s.ProjectionField[%s,%s]", f.name, query, name, types[i])
	}
	e.line("}")
	e.line("// %sFields returns typed projection output declarations.", name)
	e.line("func %sFields()%sFieldSet{return %sFieldSet{", name, name, name)
	for i, f := range projection.fields {
		e.line("%s:%s.NewProjectionField[%s,%s](%q),", f.name, query, name, types[i], f.column)
	}
	e.line("}}")
	e.line("// %sProjection provides the complete result schema and generated decoder.", name)
	e.line("func %sProjection()%s.ProjectionDefinition[%s]{%s:=%sFields();return %s.DefineProjection([]%s.ProjectionColumn[%s]{", name, query, name, fieldsVar, name, query, query, name)
	for _, f := range projection.fields {
		e.line("%s.%s.Column(),", fieldsVar, f.name)
	}
	e.line("},func(%s %s.Row)(%s,error){var %s %s;if err:=%s.Scan(", rowVar, database, name, itemVar, name, rowVar)
	for i, f := range projection.fields {
		e.line("%s.Scan(&%s.%s),", codecs[i], itemVar, f.name)
	}
	e.line(");err!=nil{return %s{},err};return %s,nil},", name, itemVar)
	for i, f := range projection.fields {
		e.line("%s.NewRecordField(%q,%s,func(item %s)%s{return item.%s}),", query, f.column, codecs[i], name, types[i], f.name)
	}
	e.line(")}")
	e.line("// %sSelection requires a type-compatible expression for every result field.", name)
	e.line("type %sSelection[%s any]struct{", name, scope)
	for i, f := range projection.fields {
		e.line("%s %s.Expression[%s,%s]", f.name, query, scope, types[i])
	}
	e.line("}")
	outer, inner := e.localName("FoundryOuter"), e.localName("FoundryInner")
	for _, kind := range projectionKinds() {
		shape := kind.shape(name, query, scope, outer, inner)
		selectName, projectName, _ := kind.names(name)
		e.line("// %s retains source ownership and exact result field types.", selectName)
		e.line("func %s[%s](source %s,selection %sSelection[%s])%s{fields:=%sFields();return %s.%s(source,%sProjection(),", selectName, shape.declaration, shape.source, name, shape.scope, shape.result, name, query, shape.project, name)
		for _, f := range projection.fields {
			e.line("%s.Map(fields.%s,selection.%s),", query, f.name, f.name)
		}
		e.line(")}")
		e.line("// %s infers input ownership and selects typed result fields.", shape.builder)
		e.line("type %s[%s]struct{source %s;selection %sSelection[%s]}", shape.builder, shape.declaration, shape.source, name, shape.scope)
		e.line("// %s starts a fluent projection selection without executing it.", projectName)
		e.line("func %s[%s](source %s)%s[%s]{return %s[%s]{source:source}}", projectName, shape.declaration, shape.source, shape.builder, shape.arguments, shape.builder, shape.arguments)
		for i, f := range projection.fields {
			e.line("// Select%s supplies the typed expression for %s.", f.name, f.column)
			e.line("func(p %s[%s])Select%s(v %s.Expression[%s,%s])%s[%s]{p.selection.%s=v;return p}", shape.builder, shape.arguments, f.name, query, shape.scope, types[i], shape.builder, shape.arguments, f.name)
		}
		e.line("// Query retains the source requirements and complete selection mapping.")
		e.line("func(p %s[%s])Query()%s{return %s(p.source,p.selection)}", shape.builder, shape.arguments, shape.result, selectName)
	}
	e.emitScopedFields(name, projection.fields, name, "RecordScope", "NullableRecordScope")
	if dto != nil {
		e.emitDTO(*dto)
	}
	return e.finish(projection.position.Filename, projection.position.Line)
}
