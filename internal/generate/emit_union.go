package generate

import (
	"fmt"
	"strings"
)

func emitUnion(p *packageInput, d unionDeclaration) ([]byte, error) {
	e := newEmitter(p)
	contract := e.useNamed(framework+"/contract", "foundrycontract")
	fault := e.useNamed(framework+"/fault", "foundryfault")
	e.line("// %s is a closed immutable tagged union. Use its typed constructors and accessors.", d.name)
	e.line("// Accessors return fresh payloads; zero is invalid except inside Optional/Nullable.")
	e.line("type %s struct{value %s.UnionValue[%s]}", d.name, contract, d.name)
	e.line("// %sKind is the selected wire discriminator.", d.name)
	e.line("type %sKind string", d.name)
	e.line("const (")
	for _, variant := range d.variants {
		e.line("%s%sKind %sKind=%q", d.name, variant.name, d.name, variant.tag)
	}
	e.line(")")
	e.line("func(v %s)Kind()%sKind{return %sKind(v.value.Tag())}", d.name, d.name, d.name)
	e.line("func(v %s)IsZero()bool{return v.value.IsZero()}", d.name)
	e.line("func(v %s)MarshalJSON()([]byte,error){return v.value.MarshalJSON()}", d.name)
	e.line("func(v %s)JSONContract()%s.JSON[%s]{return %sJSON()}", d.name, contract, d.name, d.name)
	e.line("// %sJSON returns the immutable shared contract for this union.", d.name)
	e.line("func %sJSON()%s.JSON[%s]{return foundry%sContract}", d.name, contract, d.name, d.name)
	e.line("var foundry%sContract=build%sContract()", d.name, d.name)
	e.line("func build%sContract()%s.JSON[%s]{", d.name, contract, d.name)
	fmt.Fprint(&e.body, "return ")
	e.emitJSONValue(d.typ, d.dtoGraph, "DefineJSONValue")
	e.line("}")
	for _, variant := range d.variants {
		typ := e.typeName(variant.typ)
		e.line("// %sFrom%s validates and captures an owned %s payload.", d.name, variant.name, typ)
		e.line("func %sFrom%s(input %s)(%s,error){", d.name, variant.name, typ, d.name)
		e.line("stored,err:=%s.EncodeUnion(%sJSON(),%q,input);if err!=nil{return %s{},err}", contract, d.name, variant.tag, d.name)
		e.line("if _,err:=%s.UnionVariant[%s,%s](stored,%q);err!=nil{return %s{},err}", contract, d.name, typ, variant.tag, d.name)
		e.line("return %s{value:stored},nil}", d.name)
		e.line("// %s returns a fresh typed payload when this variant is selected.", variant.name)
		e.line("func(v %s)%s()(%s,bool){payload,err:=%s.UnionVariant[%s,%s](v.value,%q);return payload,err==nil}", d.name, variant.name, typ, contract, d.name, typ, variant.tag)
	}
	e.line("func(v *%s)UnmarshalJSON(data []byte)error{", d.name)
	e.line("if v==nil{return %s.New(%s.Invalid,\"invalid union receiver\")}", fault, fault)
	e.line("stored,err:=%s.DecodeUnion(%sJSON(),data);if err!=nil{return err}", contract, d.name)
	e.line("switch stored.Tag(){")
	for _, variant := range d.variants {
		e.line("case %q:_,err=%s.UnionVariant[%s,%s](stored,%q)", variant.tag, contract, d.name, e.typeName(variant.typ), variant.tag)
	}
	e.line("}")
	e.line("if err!=nil{return err};*v=%s{value:stored};return nil}", d.name)
	var arguments, checks []string
	for _, variant := range d.variants {
		name := "on" + variant.name
		arguments = append(arguments, name+" func("+e.typeName(variant.typ)+")(R,error)")
		checks = append(checks, name+"==nil")
	}
	e.line("// Match%s requires one typed callback per variant; adding a variant changes this signature.", d.name)
	e.line("func Match%s[R any](input %s,%s)(R,error){", d.name, d.name, strings.Join(arguments, ","))
	e.line("if %s{return *new(R),%s.New(%s.Invalid,\"union match requires every variant callback\")}", strings.Join(checks, "||"), fault, fault)
	e.line("switch input.Kind(){")
	for _, variant := range d.variants {
		e.line("case %s%sKind:payload,err:=%s.UnionVariant[%s,%s](input.value,%q);if err!=nil{return *new(R),err};return on%s(payload)", d.name, variant.name, contract, d.name, e.typeName(variant.typ), variant.tag, variant.name)
	}
	e.line("}")
	e.line("return *new(R),%s.New(%s.Invalid,\"invalid union value\")}", fault, fault)
	return e.finish(d.position.Filename)
}
