package generate

func emitQuery(p *packageInput, declaration queryDeclaration) ([]byte, error) {
	e := newEmitter(p)
	http := e.useNamed(framework+"/http", "foundryhttp")
	for _, field := range declaration.fields {
		_ = e.typeName(field.typ)
		_ = e.typeName(field.element)
		if field.scalar.owner != nil {
			_ = e.typeName(field.scalar.owner)
		}
	}
	variable := e.localName("query")
	if declaration.source == "form" {
		e.line("// %sDescriptor binds URL-encoded form fields to concrete %s values.", declaration.name, declaration.name)
		e.line("// Pass these shared URL bindings to FormBody; body and query remain independent.")
	} else {
		e.line("// %sDescriptor binds query parameters to concrete %s fields.", declaration.name, declaration.name)
	}
	e.line("// Reuse this descriptor for request decoding and query URL encoding.")
	e.line("// Optional fields preserve omission; repeated fields retain slice order.")
	e.line("func %sDescriptor() %s.Query[%s] {return foundry%sDescriptor()}", declaration.name, http, declaration.name, declaration.name)
	e.line("var foundry%sDescriptor=%s.OnceValue(func()%s.Query[%s]{", declaration.name, e.use("sync"), http, declaration.name)
	e.line("return %s.DefineQuery[%s](", http, declaration.name)
	for _, field := range declaration.fields {
		codec, err := e.urlScalarCodec(http, "Query", field.element, field.scalar)
		if err != nil {
			return nil, err
		}
		typeArguments := declaration.name + "," + e.typeName(field.element)
		if field.binding == "RepeatedQueryParam" {
			typeArguments += "," + e.typeName(field.typ)
		}
		e.line("%s.%s[%s](%q,%s,func(%s *%s)*%s{return &%s.%s}),", http, field.binding, typeArguments, field.parameter, codec, variable, declaration.name, e.typeName(field.typ), variable, field.name)
	}
	e.line(")})")
	if declaration.source == "form" {
		e.emitTransportValidation(declaration.name, declaration.typ, declaration.position, declaration.validationProperties(), "form", "field")
	}
	return e.finish(declaration.position.Filename)
}
