package generate

func emitPath(p *packageInput, declaration pathDeclaration) ([]byte, error) {
	e := newEmitter(p)
	http := e.useNamed(framework+"/http", "foundryhttp")
	// Allocate import aliases before a local selector name, using the same
	// allocator that protects generated model code from consumer type shadowing.
	for _, field := range declaration.fields {
		_ = e.typeName(field.typ)
		if field.owner != nil {
			_ = e.typeName(field.owner)
		}
	}
	variable := e.localName("path")
	e.line("// %sDescriptor binds the declared pattern to concrete %s fields.", declaration.name, declaration.name)
	e.line("// Reuse this descriptor for route registration and named URL generation.")
	e.line("func %sDescriptor() %s.Path[%s] {", declaration.name, http, declaration.name)
	e.line("return %s.DefinePath[%s](%q,", http, declaration.name, declaration.pattern)
	for _, field := range declaration.fields {
		typ := e.typeName(field.typ)
		codec, err := e.urlScalarCodec(http, "Path", field.typ, urlScalar{kind: field.codec, owner: field.owner})
		if err != nil {
			return nil, err
		}
		e.line("%s.Param[%s,%s](%q,%s,func(%s *%s)*%s{return &%s.%s}),", http, declaration.name, typ, field.parameter, codec, variable, declaration.name, typ, variable, field.name)
	}
	e.line(")}")
	return e.finish(declaration.position.Filename, declaration.position.Line)
}
