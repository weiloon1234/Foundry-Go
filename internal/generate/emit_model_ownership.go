package generate

// Only binary model/input values need an ownership adapter. Custom struct
// inputs keep the existing read-only reference contract; no reflection or
// arbitrary deep-copy policy is introduced into model generation.
func (e *emitter) binaryInputCodec(f field, nullable bool) string {
	typ := f.base
	if f.input != nil {
		typ = f.input
	}
	if !binaryType(typ) {
		return ""
	}
	codec := e.use(framework + "/database/codec")
	expression := codec + ".Bytes[" + e.typeName(typ) + "]()"
	if nullable && f.nullable {
		expression = codec + ".Nullable(" + expression + ")"
	}
	return expression
}

func (e *emitter) cloneDraftInput(f field, expression string, optional bool) string {
	c := e.binaryInputCodec(f, optional)
	if c == "" {
		return expression
	}
	method := ".Clone("
	if optional {
		method = ".CloneOptional("
	}
	return c + method + expression + ")"
}

func (e *emitter) cloneStoredField(f field, expression string) string {
	if f.kind == "Binary" {
		return e.fieldCodec(f, true) + ".Clone(" + expression + ")"
	}
	return expression
}

func hasBinaryFields(m model) bool {
	for _, f := range m.fields {
		if f.kind == "Binary" {
			return true
		}
	}
	return false
}
