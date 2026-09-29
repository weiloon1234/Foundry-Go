package generate

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type emitter struct {
	pkg         *packageInput
	imports     map[string]string
	reserved    map[string]bool
	genericKeys map[*types.TypeParam]string
	body        bytes.Buffer
	err         error
}

func newEmitter(p *packageInput) *emitter {
	return &emitter{pkg: p, imports: make(map[string]string), reserved: make(map[string]bool)}
}
func (e *emitter) use(importPath string) string {
	return e.useNamed(importPath, path.Base(importPath))
}
func (e *emitter) useNamed(importPath, base string) string {
	if alias, ok := e.imports[importPath]; ok {
		return alias
	}
	if base == "query" || base == "value" || base == "fault" {
		base = "foundry" + base
	}
	alias := base
	for n := 1; e.reserved[alias] || e.pkg.defined[alias] || e.pkg.types.Scope().Lookup(alias) != nil || types.Universe.Lookup(alias) != nil || e.hasAlias(alias); n++ {
		alias = base + strconv.Itoa(n)
	}
	e.imports[importPath] = alias
	return alias
}
func (e *emitter) hasAlias(alias string) bool {
	for _, used := range e.imports {
		if used == alias {
			return true
		}
	}
	return false
}

// localName prevents generated locals/type parameters from shadowing consumer
// types that are referenced by generated codec expressions in the same body.
func (e *emitter) localName(base string) string {
	name := base
	for n := 1; e.reserved[name] || e.pkg.defined[name] || e.pkg.types.Scope().Lookup(name) != nil || types.Universe.Lookup(name) != nil || e.hasAlias(name); n++ {
		name = base + strconv.Itoa(n)
	}
	return name
}
func (e *emitter) typeName(typ types.Type) string {
	return types.TypeString(typ, func(pkg *types.Package) string {
		if pkg.Path() == e.pkg.path {
			return ""
		}
		return e.useNamed(pkg.Path(), pkg.Name())
	})
}
func (e *emitter) line(pattern string, args ...any) { fmt.Fprintf(&e.body, pattern+"\n", args...) }

// finish names the declaration's source file without a line number, so edits
// above a declaration do not rewrite every generated file below it.
func (e *emitter) finish(source string) ([]byte, error) {
	if e.err != nil {
		return nil, e.err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n// Source: %s.\n\npackage %s\n\n", generatedHeader, path.Base(filepath.ToSlash(source)), e.pkg.name)
	if len(e.imports) > 0 {
		out.WriteString("import (\n")
		for _, p := range sortedNames(e.imports) {
			fmt.Fprintf(&out, "%s %q\n", e.imports[p], p)
		}
		out.WriteString(")\n\n")
	}
	out.Write(e.body.Bytes())
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w", err)
	}
	return formatted, nil
}

// emit renders each declaration's owned file and records the declaration
// position that produced it for collision diagnostics.
func emit(p *packageInput, metadata metadata) (map[string][]byte, map[string]token.Position, error) {
	outputs := make(map[string][]byte)
	origins := make(map[string]token.Position)
	var position token.Position
	add := func(name string, data []byte, err error) error {
		if err != nil {
			return err
		}
		file := snake(name) + "_foundry.gen.go"
		if previous, exists := origins[file]; exists {
			return fmt.Errorf("%s: declaration %s produces %s, which the declaration at %s already produces; rename one declaration", position, name, file, previous)
		}
		outputs[file] = data
		origins[file] = position
		return nil
	}
	for _, declaration := range metadata.unions {
		position = declaration.position
		data, err := emitUnion(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, declaration := range metadata.configs {
		position = declaration.position
		data, err := emitConfig(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, m := range metadata.models {
		position = m.position
		data, err := emitModel(p, m)
		if err := add(m.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, enum := range metadata.enums {
		position = enum.position
		data, err := emitEnum(p, enum)
		if err := add(enum.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, projection := range metadata.projections {
		position = projection.position
		var dto *dtoDeclaration
		for i := range metadata.dtos {
			if metadata.dtos[i].projected && metadata.dtos[i].name == projection.name {
				dto = &metadata.dtos[i]
				break
			}
		}
		data, err := emitProjection(p, projection, dto)
		if err := add(projection.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, declaration := range metadata.paths {
		position = declaration.position
		data, err := emitPath(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, declaration := range metadata.queries {
		position = declaration.position
		data, err := emitQuery(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, declaration := range metadata.multipart {
		position = declaration.position
		data, err := emitMultipart(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	for _, declaration := range metadata.dtos {
		if declaration.projected {
			continue
		}
		position = declaration.position
		data, err := emitDTO(p, declaration)
		if err := add(declaration.name, data, err); err != nil {
			return nil, nil, err
		}
	}
	return outputs, origins, nil
}

func emitModel(p *packageInput, m model) ([]byte, error) {
	e := newEmitter(p)
	value := e.use(framework + "/value")
	e.emitModelFields(m)
	e.emitModelQuery(m)
	draftVar, inputVar := e.localName("d"), e.localName("v")
	e.line("// %sDraft tracks explicitly selected mutation fields, preserving omitted, zero and null states.", m.name)
	e.line("type %sDraft struct {", m.name)
	for _, f := range m.fields {
		e.line("field%s %s.Optional[%s]", f.name, value, e.fieldInputType(f, true))
	}
	e.line("}")
	for _, f := range m.fields {
		e.line("// %s returns the typed mutation state for %s.", f.name, f.column)
		e.emitFieldBehavior(m.name, m.table, f)
		e.line("func(%s %sDraft)%s()%s.Optional[%s]{return %s}", draftVar, m.name, f.name, value, e.fieldInputType(f, true), e.cloneDraftInput(f, draftVar+".field"+f.name, true))
		e.line("// Set%s returns a new draft with %s explicitly set.", f.name, f.column)
		e.emitFieldBehavior(m.name, m.table, f)
		assignment := e.cloneDraftInput(f, inputVar, false)
		if f.nullable {
			assignment = value + ".Of(" + assignment + ")"
		}
		e.line("func(%s %sDraft)Set%s(%s %s)%sDraft{%s.field%s=%s.Set(%s);return %s}", draftVar, m.name, f.name, inputVar, e.fieldInputType(f, false), m.name, draftVar, f.name, value, assignment, draftVar)
		e.line("// Unset%s omits %s without requesting a zero or null write.", f.name, f.column)
		e.line("func(%s %sDraft)Unset%s()%sDraft{%s.field%s=%s.Optional[%s]{};return %s}", draftVar, m.name, f.name, m.name, draftVar, f.name, value, e.fieldInputType(f, true), draftVar)
		if f.nullable {
			e.line("// Clear%s explicitly sets %s to null.", f.name, f.column)
			e.line("func(%s %sDraft)Clear%s()%sDraft{%s.field%s=%s.Set(%s.Null[%s]());return %s}", draftVar, m.name, f.name, m.name, draftVar, f.name, value, value, e.fieldInputType(f, false), draftVar)
		}
	}
	e.line("// IsEmpty reports whether no fields have been selected for mutation.")
	var checks []string
	for _, f := range m.fields {
		checks = append(checks, "!"+draftVar+".field"+f.name+".IsSet()")
	}
	e.line("func(%s %sDraft)IsEmpty()bool{return %s}", draftVar, m.name, strings.Join(checks, "&&"))
	formatter := e.use("fmt")
	state := e.localName("state")
	e.line("// Format omits pending draft values, including sensitive mutation inputs, from routine diagnostics.")
	e.line("func(%sDraft)Format(%s %s.State,_ rune){_,_=%s.Write([]byte(%q))}", m.name, state, formatter, state, m.name+" draft")
	e.emitModelChanges(m)
	e.emitModelHooks(m)
	e.emitModelRetrievalHooks(m)
	e.emitModelAudit(m)
	return e.finish(m.position.Filename)
}
func fieldClass(f field) string {
	if f.nullable {
		if f.kind == "Scalar" {
			return "NullableField"
		}
		return "Nullable" + f.kind + "Field"
	}
	return f.kind + "Field"
}

func emitEnum(p *packageInput, enum enum) ([]byte, error) {
	e := newEmitter(p)
	fault, json := e.use(framework+"/fault"), e.use("encoding/json")
	enumPackage, driver := e.use(framework+"/enum"), e.use("database/sql/driver")
	base := enum.base.Name()
	e.line("// %sValues returns declared values in source order, with independent slice ownership.", enum.name)
	var names []string
	for _, v := range enum.values {
		names = append(names, v.name)
	}
	e.line("func %sValues()[]%s{return []%s{%s}}", enum.name, enum.name, enum.name, strings.Join(names, ","))
	e.line("// EnumDescriptor describes this enum type independently of the receiver's value.")
	e.line("func(%s)EnumDescriptor()%s.Descriptor[%s]{return foundry%sEnumDescriptor()}", enum.name, enumPackage, enum.name, enum.name)
	e.line("var foundry%sEnumDescriptor=%s.OnceValue(func()%s.Descriptor[%s]{return %s.Describe(%q,%q,", enum.name, e.use("sync"), enumPackage, enum.name, enumPackage, p.path, enum.name)
	for _, v := range enum.values {
		label := ""
		if enum.labels != "" {
			label = enum.labels + "." + snake(v.name)
		}
		suffix := ""
		if label != "" {
			suffix = fmt.Sprintf(",LabelKey:%q", label)
		}
		e.line("%s.Case[%s]{Name:%q,Value:%s%s},", enumPackage, enum.name, v.name, v.name, suffix)
	}
	e.line(")})")
	e.line("// IsValid checks membership; a Go cast alone does not validate external values.")
	e.line("func(v %s)IsValid()bool{switch v{case %s:return true};return false}", enum.name, strings.Join(names, ","))
	e.line("// Validate reports invalid values without formatting the input.")
	e.line("func(v %s)Validate()error{if !v.IsValid(){return %s.New(%s.Invalid,%q)};return nil}", enum.name, fault, fault, "invalid "+enum.name+" value")
	e.line("// Parse%s decodes and validates an enum's text representation.", enum.name)
	e.line("func Parse%s(text string)(%s,error){", enum.name, enum.name)
	textValue := "string(v)"
	if enum.base.Kind() == types.String {
		e.line("v:=%s(text)", enum.name)
	} else {
		str := e.use("strconv")
		bits := integerBits(enum.base, str)
		parse, format, convert := "ParseInt", "FormatInt", "int64"
		if enum.base.Info()&types.IsUnsigned != 0 {
			parse, format, convert = "ParseUint", "FormatUint", "uint64"
		}
		e.line("raw,err:=%s.%s(text,10,%s);if err!=nil{return 0,%s.Wrap(%s.Invalid,%q,err)}", str, parse, bits, fault, fault, "invalid "+enum.name+" representation")
		e.line("v:=%s(raw)", enum.name)
		textValue = fmt.Sprintf("%s.%s(%s(v),10)", str, format, convert)
	}
	e.line("if err:=v.Validate();err!=nil{return *new(%s),err};return v,nil}", enum.name)
	e.line("func(v %s)MarshalText()([]byte,error){if err:=v.Validate();err!=nil{return nil,err};return []byte(%s),nil}", enum.name, textValue)
	e.line("func(v *%s)UnmarshalText(data []byte)error{parsed,err:=Parse%s(string(data));if err!=nil{return err};*v=parsed;return nil}", enum.name, enum.name)
	e.line("func(v %s)MarshalJSON()([]byte,error){if err:=v.Validate();err!=nil{return nil,err};return %s.Marshal(%s(v))}", enum.name, json, base)
	e.line("func(v *%s)UnmarshalJSON(data []byte)error{var raw *%s;if err:=%s.Unmarshal(data,&raw);err!=nil{return %s.Wrap(%s.Invalid,%q,err)};if raw==nil{return %s.New(%s.Invalid,%q)};parsed:=%s(*raw);if err:=parsed.Validate();err!=nil{return err};*v=parsed;return nil}", enum.name, base, json, fault, fault, "invalid "+enum.name+" representation", fault, fault, "enum cannot decode null", enum.name)
	codec := e.use(framework + "/database/codec")
	e.line("// %sCodec applies the generated membership rule at both SQL boundaries.", enum.name)
	e.line("func %sCodec()%s.Codec[%s]{return %s}", enum.name, codec, enum.name, e.fieldCodec(field{base: enum.typ, enum: true}, false))
	e.line("// Value implements database/sql/driver.Valuer through the shared typed codec.")
	e.line("func(v %s)Value()(%s.Value,error){return %sCodec().Bind(v)}", enum.name, driver, enum.name)
	e.line("// Scan changes the receiver only after successful decoding. SQL NULL is rejected.")
	e.line("func(v *%s)Scan(source any)error{return %sCodec().Scan(v).Scan(source)}", enum.name, enum.name)
	return e.finish(enum.position.Filename)
}
func integerBits(base *types.Basic, strconv string) string {
	switch base.Kind() {
	case types.Int8, types.Uint8:
		return "8"
	case types.Int16, types.Uint16:
		return "16"
	case types.Int32, types.Uint32:
		return "32"
	case types.Int64, types.Uint64:
		return "64"
	}
	return strconv + ".IntSize"
}
