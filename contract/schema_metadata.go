package contract

// Normalize validates and copies a possibly serialized schema for inspection
// and client export. It shares the runtime graph compiler, including reference,
// alias-cycle, enum, resource and property checks. It does not install native
// Go map-key codecs: metadata alone cannot create a typed runtime JSON binding.
func (s Schema) Normalize() (Schema, error) {
	compiled, err := compileSchemaMode(s, true)
	if err != nil {
		return Schema{}, err
	}
	return compiled.snapshot(), nil
}

func normalizeKeyMetadata(key JSONKeyInfo) (JSONKeyInfo, error) {
	typ := key.Value
	if typ.Nullable || typ.Key != nil || (typ.Kind != StringKind && typ.Kind != IntegerKind) {
		return JSONKeyInfo{}, invalidSchema()
	}
	scalar, err := compileSchema(Schema{Root: typ.ID, Types: []Type{typ}})
	if err != nil {
		return JSONKeyInfo{}, err
	}
	key.Value = scalar.snapshot().Types[0]
	switch key.Syntax {
	case StringJSONKeySyntax:
		if typ.Kind != StringKind || key.NonZero {
			return JSONKeyInfo{}, invalidSchema()
		}
	case IntegerJSONKeySyntax:
		if typ.Kind != IntegerKind || key.NonZero {
			return JSONKeyInfo{}, invalidSchema()
		}
	case EnumJSONKeySyntax:
		if len(key.Value.Cases) == 0 || key.NonZero {
			return JSONKeyInfo{}, invalidSchema()
		}
	case ModelIDJSONKeySyntax:
		if typ.Kind != StringKind || typ.Format != UUIDFormat || !key.NonZero {
			return JSONKeyInfo{}, invalidSchema()
		}
	case CustomJSONKeySyntax:
		if !key.ServerOnly || key.NonZero {
			return JSONKeyInfo{}, invalidSchema()
		}
	default:
		return JSONKeyInfo{}, invalidSchema()
	}
	return key, nil
}
