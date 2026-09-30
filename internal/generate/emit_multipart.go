package generate

import "fmt"

func emitMultipart(p *packageInput, declaration multipartDeclaration) ([]byte, error) {
	e := newEmitter(p)
	http := e.useNamed(framework+"/http", "foundryhttp")
	// Retain source aliases such as http.UploadedFile. Resolving their private
	// implementation identity must not produce inaccessible consumer imports.
	for _, field := range declaration.fields {
		_ = e.typeName(field.typ)
	}
	e.line("// %sDescriptor binds multipart parts to concrete %s fields.", declaration.name, declaration.name)
	e.line("// Files are request-owned; Optional preserves omission and slices retain part order.")
	e.line("// Structured JSON parts use explicit contracts; models are not transport DTOs.")
	e.line("func %sDescriptor()%s.Multipart[%s]{return foundry%sDescriptor()}", declaration.name, http, declaration.name, declaration.name)
	e.line("var foundry%sDescriptor=%s.OnceValue(func()%s.Multipart[%s]{", declaration.name, e.use("sync"), http, declaration.name)
	e.line("return %s.DefineMultipart[%s](", http, declaration.name)
	for _, field := range declaration.fields {
		switch field.kind {
		case "text":
			codec, err := e.urlScalarCodec(http, "Query", field.element, field.query.scalar)
			if err != nil {
				return nil, err
			}
			args := declaration.name + "," + e.typeName(field.element)
			if field.binding == "RepeatedQueryParam" {
				args += "," + e.typeName(field.typ)
			}
			input := e.localName("form")
			e.line("%s.TextPart(%s.%s[%s](%q,%s,func(%s *%s)*%s{return &%s.%s}))%s,", http, http, field.binding, args, field.parameter, codec, input, declaration.name, e.typeName(field.typ), input, field.name, e.presentationMethod(field.presentation))
		case "file":
			args := declaration.name
			if field.binding == "RepeatedFilePart" {
				args += "," + e.typeName(field.typ)
			}
			input := e.localName("form")
			e.line("%s.%s[%s](%q,func(%s *%s)*%s{return &%s.%s})%s,", http, field.binding, args, field.parameter, input, declaration.name, e.typeName(field.typ), input, field.name, e.presentationMethod(field.presentation))
		case "json":
			args := declaration.name + "," + e.typeName(field.element)
			if field.binding == "RepeatedJSONPart" {
				args += "," + e.typeName(field.typ)
			}
			fmt.Fprintf(&e.body, "%s.%s[%s](%q,", http, field.binding, args, field.parameter)
			e.emitJSONValue(field.element, field.graph, "DefineJSONField")
			input := e.localName("form")
			e.line(",func(%s *%s)*%s{return &%s.%s})%s,", input, declaration.name, e.typeName(field.typ), input, field.name, e.presentationMethod(field.presentation))
		}
	}
	e.line(")})")
	e.emitTransportValidation(declaration.name, declaration.typ, declaration.position, declaration.validationProperties(), "multipart", "part")
	return e.finish(declaration.position.Filename)
}
