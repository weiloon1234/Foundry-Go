package generate

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/config"
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
				e.emitFieldHelp(field.help)
				e.line("%s %s", field.name, configKeySetName(declaration.name, field.path))
			} else {
				e.line("// %s selects %s. Set accepts %s.", field.name, field.key, e.typeName(field.typ))
				e.emitFieldHelp(field.help)
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
	e.line("func %sConfigKeys() %s {return foundry%sConfigKeys()}", declaration.name, configKeySetName(declaration.name, ""), declaration.name)
	e.line("var foundry%sConfigKeys=%s.OnceValue(func()%s{", declaration.name, e.use("sync"), configKeySetName(declaration.name, ""))
	fmt.Fprint(&e.body, "return ")
	literal("", declaration.fields)
	e.line("")
	e.line("})")
	keys := e.localName("keys")
	e.line("// %sConfigSchema reuses the shared file/environment/override loader.", declaration.name)
	e.line("func %sConfigSchema()(*%s.Schema[%s],error){", declaration.name, configuration, declaration.name)
	if len(declaration.fields) > 0 {
		e.line("%s := %sConfigKeys()", keys, declaration.name)
	}
	e.line("return %s.New[%s](", configuration, declaration.name)
	walkConfigLeaves(declaration.fields, func(field configField) { e.line("%s.%s,", keys, field.path) })
	e.line(")}")
	if len(declaration.publicSettings) > 0 {
		e.line("// %sPublicConfig returns the public configuration declared on %s.", declaration.name, declaration.name)
		e.line("func %sPublicConfig() []%s.PublicSetting {", declaration.name, configuration)
		e.line("return []%s.PublicSetting{", configuration)
		for _, s := range declaration.publicSettings {
			casesLit := "nil"
			if len(s.EnumCases) > 0 {
				quoted := make([]string, len(s.EnumCases))
				for i, c := range s.EnumCases {
					quoted[i] = fmt.Sprintf("%q", c)
				}
				casesLit = fmt.Sprintf("[]string{%s}", strings.Join(quoted, ", "))
			}
			kindConst := ""
			switch s.Kind {
			case config.PublicString:
				kindConst = configuration + ".PublicString"
			case config.PublicBoolean:
				kindConst = configuration + ".PublicBoolean"
			case config.PublicNumber:
				kindConst = configuration + ".PublicNumber"
			case config.PublicLocale:
				kindConst = configuration + ".PublicLocale"
			case config.PublicCurrency:
				kindConst = configuration + ".PublicCurrency"
			case config.PublicTimeZone:
				kindConst = configuration + ".PublicTimeZone"
			case config.PublicEnum:
				kindConst = configuration + ".PublicEnum"
			default:
				kindConst = fmt.Sprintf("%s.PublicKind(%q)", configuration, s.Kind)
			}
			e.line("{Name: %q, Env: %q, Description: %q, Fallback: %q, Kind: %s, MaxLength: %d, EnumCases: %s},",
				s.Name, s.Env, s.Description, s.Fallback, kindConst, s.MaxLength, casesLit)
		}
		e.line("}")
		e.line("}")
	}
	return e.finish(declaration.position.Filename)
}
