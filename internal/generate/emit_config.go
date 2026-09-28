package generate

import (
	"fmt"
	"go/types"
)

func emitConfig(p *packageInput, declaration configDeclaration) ([]byte, error) {
	e := newEmitter(p)
	configuration := e.use(framework + "/config")
	walkConfigLeaves(declaration.fields, func(field configField) { _ = e.typeName(field.typ) })
	var emitSet func(string, []configField)
	emitSet = func(path string, fields []configField) {
		e.line("// %s exposes compiler-checked configuration overrides.", configKeySetName(declaration.name, path))
		e.line("type %s struct {", configKeySetName(declaration.name, path))
		for _, field := range fields {
			if len(field.children) > 0 {
				e.line("%s %s", field.name, configKeySetName(declaration.name, field.path))
			} else {
				e.line("// %s selects %s. Set accepts %s.", field.name, field.key, e.typeName(field.typ))
				e.line("%s %s.Key[%s,%s]", field.name, configuration, declaration.name, e.typeName(field.typ))
			}
		}
		e.line("}")
		for _, field := range fields {
			if len(field.children) > 0 {
				emitSet(field.path, field.children)
			}
		}
	}
	emitSet("", declaration.fields)
	variable := e.localName("settings")
	var literal func(string, []configField)
	literal = func(path string, fields []configField) {
		e.line("%s{", configKeySetName(declaration.name, path))
		for _, field := range fields {
			if len(field.children) > 0 {
				e.line("%s:", field.name)
				literal(field.path, field.children)
				e.line(",")
				continue
			}
			valueType := e.typeName(field.typ)
			arguments := declaration.name + "," + valueType
			extra := ""
			switch field.codec {
			case "Duration", "Secret":
				arguments = declaration.name
			case "Text":
				arguments += ",*" + valueType
			case "Enum":
				zero := "0"
				if field.typ.Underlying().(*types.Basic).Kind() == types.String {
					zero = `""`
				}
				extra = fmt.Sprintf(",(%s(%s)).EnumDescriptor()", valueType, zero)
			}
			sensitive := ""
			if field.secret {
				sensitive = ".Sensitive()"
			}
			e.line("%s: %s.%s[%s](%q,func(%s *%s)*%s{return &%s.%s}%s)%s,", field.name, configuration, field.codec, arguments, field.key, variable, declaration.name, valueType, variable, field.path, extra, sensitive)
		}
		fmt.Fprint(&e.body, "}")
	}
	e.line("// %sConfigKeys derives keys from the handwritten settings declaration.", declaration.name)
	e.line("func %sConfigKeys() %s {", declaration.name, configKeySetName(declaration.name, ""))
	fmt.Fprint(&e.body, "return ")
	literal("", declaration.fields)
	e.line("")
	e.line("}")
	keys := e.localName("keys")
	e.line("// %sConfigSchema reuses the shared file/environment/override loader.", declaration.name)
	e.line("func %sConfigSchema()(*%s.Schema[%s],error){", declaration.name, configuration, declaration.name)
	if len(declaration.fields) > 0 {
		e.line("%s := %sConfigKeys()", keys, declaration.name)
	}
	e.line("return %s.New[%s](", configuration, declaration.name)
	walkConfigLeaves(declaration.fields, func(field configField) { e.line("%s.%s,", keys, field.path) })
	e.line(")}")
	return e.finish(declaration.position.Filename, declaration.position.Line)
}
