package generate

import (
	"fmt"
	"go/types"

	foundrycontract "github.com/weiloon1234/Foundry-Go/contract"
)

func emitDTO(p *packageInput, declaration dtoDeclaration) ([]byte, error) {
	e := newEmitter(p)
	e.emitDTO(declaration)
	return e.finish(declaration.position.Filename)
}

func (e *emitter) emitDTO(declaration dtoDeclaration) {
	parameters := genericParameters(declaration.typ)
	for i := range parameters.Len() {
		e.reserved[parameters.At(i).Obj().Name()] = true
	}
	contract := e.useNamed(framework+"/contract", "foundrycontract")
	e.line("// %sJSON describes the concrete %s JSON boundary.", declaration.name, declaration.name)
	e.line("// Reuse this immutable descriptor for decoding and client contract export.")
	e.line("// Fields are discovered from Go declarations; persistence models are not response DTOs.")
	params, args := e.genericDeclaration(declaration.typ)
	argumentDecl, arguments := "", map[*types.TypeParam]string(nil)
	if params != "" {
		argumentDecl, arguments = e.genericArguments(declaration.typ, declaration.dtoGraph)
	}
	if params == "" {
		// Non-generic descriptors are immutable; compile the schema once.
		e.line("func %sJSON()%s.JSON[%s]{return foundry%sJSON()}", declaration.name, contract, declaration.name, declaration.name)
		e.line("var foundry%sJSON=%s.OnceValue(func()%s.JSON[%s]{", declaration.name, e.use("sync"), contract, declaration.name)
		fmt.Fprint(&e.body, "return ")
		e.emitJSONValue(declaration.typ, declaration.dtoGraph, "DefineJSON")
		e.line("})")
	} else {
		e.line("func %sJSON%s(%s)%s.JSON[%s%s]{", declaration.name, params, argumentDecl, contract, declaration.name, args)
		fmt.Fprint(&e.body, "return ")
		e.emitGenericJSONValue(declaration.typ, declaration.dtoGraph, arguments)
		e.line("}")
	}
	if declaration.role != dtoResponseRole {
		e.emitDTOValidation(declaration)
	}
	if declaration.message != nil {
		message := e.use(framework + "/i18n/message")
		e.line("// %sMessage retains this declaration's exact argument type and shared JSON contract.", declaration.name)
		e.line("func %sMessage()%s.Message[%s]{return foundry%sMessage()}", declaration.name, message, declaration.name, declaration.name)
		e.line("var foundry%sMessage=%s.OnceValue(func()%s.Message[%s]{return %s.Define(%q,%sJSON(),%s.Options{Plural:%q,Kind:%q})})", declaration.name, e.use("sync"), message, declaration.name, message, declaration.message.key, declaration.name, message, declaration.message.plural, declaration.message.kind)
	}
}

// emitJSONValue emits the same typed descriptor expression at every JSON boundary.
// The closing expression stays on the current line so an enclosing comma follows.
func (e *emitter) emitJSONValue(typ types.Type, graph dtoGraph, constructor string) {
	e.emitJSONGraph(typ, graph, constructor, nil)
}

func (e *emitter) emitGenericJSONValue(typ types.Type, graph dtoGraph, parameters map[*types.TypeParam]string) {
	e.emitJSONGraph(typ, graph, "DefineGenericJSON", parameters)
}

func (e *emitter) emitJSONGraph(typ types.Type, graph dtoGraph, constructor string, parameters map[*types.TypeParam]string) {
	contract := e.useNamed(framework+"/contract", "foundrycontract")
	identities := map[foundrycontract.TypeID]string(nil)
	if parameters != nil {
		identities = e.genericIDExpressions(graph, parameters)
	}
	id := func(value foundrycontract.TypeID) string {
		if expression, ok := identities[value]; ok {
			return expression
		}
		return fmt.Sprintf("%q", value)
	}
	prefix := ""
	if parameters != nil {
		prefix = e.genericSourceExpression(typ, parameters) + ","
	}
	e.line("%s.%s[%s](%s%s.Schema{Root:%s,Types:[]%s.Type{", contract, constructor, e.typeName(typ), prefix, contract, id(dtoTypeID(typ)), contract)
	used := make(map[*types.TypeParam]bool)
	for _, node := range graph.nodes {
		wire := node.wire
		if parameter, ok := node.typ.(*types.TypeParam); ok {
			used[parameter] = true
			e.line("%s.JSONParameter(%s,%s),", contract, id(wire.ID), parameters[parameter])
			continue
		}
		if node.codec != nil {
			typ := e.typeName(node.codec)
			e.line("%s.JSONType[%s](%s,(*new(%s)).JSONContract),", contract, typ, id(wire.ID), typ)
			continue
		}
		if node.key != nil {
			e.line("%s.JSONMapType[%s](%s,%s,%s,%s),", contract, e.typeName(node.typ), id(wire.ID), id(wire.Element), id(dtoTypeID(node.key.typ)), e.dtoMapKeyConstructor(*node.key))
			continue
		}
		if node.enum != nil {
			typ := e.typeName(node.enum)
			zero := "0"
			if node.enum.Underlying().(*types.Basic).Kind() == types.String {
				zero = `""`
			}
			e.line("%s.EnumType(%s,%s(%s).EnumDescriptor()),", contract, id(wire.ID), typ, zero)
			continue
		}
		if node.typ != nil {
			// value.List keeps its declared non-null array node below.
			named, _ := types.Unalias(node.typ).(*types.Named)
			_, list := valueList(named)
			if _, ok := types.Unalias(node.typ).Underlying().(*types.Slice); ok && hasTypeParameter(node.typ) && !list {
				e.line("%s.JSONSliceType[%s](%s,%s),", contract, e.typeName(node.typ), id(wire.ID), id(wire.Element))
				continue
			}
		}
		if inner, optional := jsonWrapper(node.typ, "Optional"); optional && hasTypeParameter(inner) {
			e.line("{ID:%s,Kind:%s.Kind(%q),Nullable:%s.IsNullableType[%s](),", id(wire.ID), contract, wire.Kind, e.use(framework+"/value"), e.typeName(inner))
		} else {
			e.line("{ID:%s,Kind:%s.Kind(%q),Nullable:%t,", id(wire.ID), contract, wire.Kind, wire.Nullable)
		}
		if wire.Discriminator != "" {
			e.line("Discriminator:%q,Variants:[]%s.Variant{", wire.Discriminator, contract)
			for _, variant := range wire.Variants {
				e.line("{Tag:%q,Type:%s},", variant.Tag, id(variant.Type))
			}
			e.line("},")
		}
		if wire.Element != "" {
			e.line("Element:%s,", id(wire.Element))
		}
		if wire.Bits != 0 {
			e.line("Bits:%d,", wire.Bits)
		}
		if wire.Signed {
			e.line("Signed:true,")
		}
		if wire.Format != "" {
			e.line("Format:%s.Format(%q),", contract, wire.Format)
		}
		if length, ok := wire.Length.Get(); ok {
			e.line("Length:%s.Set(%d),", e.use(framework+"/value"), length)
		}
		if len(wire.Properties) != 0 {
			e.line("Properties:[]%s.Property{", contract)
			for _, property := range wire.Properties {
				if property.Presentation == (foundrycontract.Presentation{}) {
					e.line("{Name:%q,Type:%s,Required:%t},", property.Name, id(property.Type), property.Required)
				} else {
					e.line("{Name:%q,Type:%s,Required:%t,Presentation:%s},", property.Name, id(property.Type), property.Required, e.presentation(property.Presentation))
				}
			}
			e.line("},")
		}
		e.line("},")
	}
	if parameters != nil {
		declared := genericParameters(typ)
		for i := range declared.Len() {
			parameter := declared.At(i)
			if !used[parameter] {
				name := parameters[parameter]
				e.line("%s.JSONParameter(%s.JSONArgumentID(%s),%s),", contract, contract, name, name)
			}
		}
	}
	fmt.Fprint(&e.body, "}})")
}
