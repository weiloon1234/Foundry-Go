package generate

import "strconv"

// fieldInputType owns generated draft and hook input spelling. Stored fields
// retain their original types/codecs; only distinct inputs need a new wrapper.
func (e *emitter) fieldInputType(f field, nullable bool) string {
	if f.input == nil {
		if nullable {
			return e.typeName(f.typ)
		}
		return e.typeName(f.base)
	}
	input := e.typeName(f.input)
	if nullable && f.nullable {
		return e.use(framework+"/value") + ".Nullable[" + input + "]"
	}
	return input
}

func inputFieldName(owner string, f field) string {
	suffix := "InputField"
	if f.nullable {
		suffix = "NullableInputField"
	}
	return owner + f.name + suffix
}

func (e *emitter) baseFieldType(owner string, f field, scope string) (name, typ string) {
	name = fieldClass(f)
	if f.kind == "JSON" {
		name = owner + f.name + name
		return name, name + "[" + scope + "]"
	}
	return name, e.use(framework+"/database/query") + "." + name + "[" + scope + "," + e.typeName(f.base) + "]"
}

func (e *emitter) baseFieldValue(owner string, f field, scope, table string) string {
	query := e.use(framework + "/database/query")
	constructor := query + ".New" + fieldClass(f) + "[" + scope + "," + e.typeName(f.base) + "](" + table + "," + strconv.Quote(f.column) + "," + e.fieldCodec(f, false) + ")"
	if f.kind == "JSON" {
		_, typ := e.baseFieldType(owner, f, scope)
		return typ + "{" + constructor + "}"
	}
	return constructor
}

// A generated wrapper overrides only the input setter. Embedding retains the
// shared stored-value operators, JSON paths and scope ownership without another
// hand-maintained family of query operations.
func (e *emitter) emitInputField(owner, table string, f field) {
	query := e.use(framework + "/database/query")
	scope := e.localName("FoundryScope")
	stored := f
	for _, nullable := range []bool{false, true} {
		if !nullable && f.nullable {
			continue
		}
		f.nullable = nullable
		name := inputFieldName(owner, f)
		_, base := e.baseFieldType(owner, f, scope)
		e.line("// %s preserves stored field operators while Set accepts the custom mutator's input.", name)
		e.line("type %s[%s any] struct{%s;input %s.MutationInputField[%s,%s]}", name, scope, base, query, scope, e.fieldInputType(f, false))
		e.line("// Set captures a fresh typed input for this field's conflict assignment; persistence transforms it once.")
		e.emitFieldBehavior(owner, table, stored)
		e.line("func(f %s[%s])Set(v %s)%s.ConflictUpdate[%s]{return f.input.Set(v)}", name, scope, e.fieldInputType(f, false), query, scope)
	}
}
