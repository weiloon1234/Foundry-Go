package generate

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func discoverModelField(p *packageInput, d *modelDeclaration, field *types.Var, tag modelmeta.ModelTag) (modelField, error) {
	f := modelField{field: field, tag: tag, stored: field.Type(), key: field.Name() == d.key, member: tag.Member}
	fail := func(message string) (modelField, error) { return f, safeerror.New(message) }
	if !field.Exported() {
		return fail(`persisted fields must be exported; use foundry:"-"`)
	}

	collision := modelTableMember(field.Name())
	if f.key {
		if tag.Member == "none" {
			return fail("member=none is redundant on the key; remove it")
		}
		if tag.Member != "" && tag.Member != field.Name() && !collision {
			return fail("member=" + tag.Member + " is redundant without a Table method collision; remove it")
		}
	} else {
		if tag.Member == field.Name() && !collision {
			return fail("member=" + tag.Member + " is redundant; remove it")
		}
		if tag.Member != "" && tag.Member != "none" && !collision {
			return fail("member=" + tag.Member + " is redundant without a Table method collision; remove it")
		}
		if tag.Member == "none" {
			f.member = ""
		} else if tag.Member == "" {
			f.member = field.Name()
		}
	}
	if f.member == "Table" {
		return fail("field collides with the generated Table field; declare member=<Name> using a non-colliding name")
	}
	if f.member != "" && modelTableMember(f.member) {
		return fail("field collides with a promoted Table method; declare member=<Name> using a non-colliding name")
	}
	if wrapper, ok := types.Unalias(f.stored).(*types.Named); ok && isNamed(wrapper, framework+"/value", "Nullable") {
		f.nullable = true
		f.stored = wrapper.TypeArgs().At(0)
	}
	n, _ := types.Unalias(f.stored).(*types.Named)
	named := func(path, name string) bool { return n != nil && isNamed(n, path, name) }
	if named(framework+"/value", "Optional") {
		return fail("Optional is a write/transport state, not a persisted column")
	}
	if named(framework+"/value", "Nullable") {
		return fail("nested Nullable is not a persisted column")
	}
	f.args = []types.Type{f.stored}
	base := ""
	instant, decimalType, encrypted, password, relation := false, false, false, false, false
	basic, _ := f.stored.Underlying().(*types.Basic)
	isString := basic != nil && basic.Info()&types.IsString != 0
	isNumber := basic != nil && basic.Info()&(types.IsInteger|types.IsFloat) != 0 && basic.Kind() != types.Uintptr
	switch {
	case n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == framework+"/database/relation":
		relation = true
		switch n.Obj().Name() {
		case "One", "Many", "Through", "Value":
			f.carrier = n.Obj().Name()
			f.constructor = f.carrier + "Of"
			f.slot = true
			f.args = nil
			for i := 0; i < n.TypeArgs().Len(); i++ {
				f.args = append(f.args, n.TypeArgs().At(i))
			}
		default:
			return fail("unsupported relation slot type " + field.Type().String())
		}
	case tag.Slot || named(framework+"/translations", "Text") || named(framework+"/metadata", "Value") || named(framework+"/attachments", "One") || named(framework+"/attachments", "Many"):
		f.slot = true
		f.carrier = "Slot"
		f.constructor = "SlotOf"
		if (named(framework+"/attachments", "One") || named(framework+"/attachments", "Many")) && !types.Identical(n.TypeArgs().At(0), d.typ) {
			return fail("attachment slot belongs to a different model")
		}
	case named(framework+"/value", "JSON"):
		base = "json"
		f.args = []types.Type{n.TypeArgs().At(0)}
	case named(framework+"/model", "ID"):
		base = "column"
		f.constructor = "IDOf"
	case named(framework+"/auth/password", "Hash"):
		base = "column"
		f.constructor = "PasswordOf"
		password = true
	case named(framework+"/database/encrypted", "Text") || named(framework+"/database/encrypted", "JSON"):
		base = "encrypted"
		encrypted = true
		f.constructor = "Encrypted" + n.Obj().Name() + "Of"
	case named(framework+"/decimal", "Decimal"):
		base = "number"
		decimalType = true
		isNumber = true
	case named("time", "Time") || named(framework+"/temporal", "DateTime"):
		base = "time"
		instant = true
	case named(framework+"/temporal", "Date") || named(framework+"/temporal", "Time") || named(framework+"/temporal", "LocalDateTime"):
		base = "time"
	case named(framework+"/temporal", "Interval"):
		base = "interval"
		f.args = nil
	case n != nil && (p.enumTypes[n] || hasEnumDescriptor(n)):
		base = "enum"
	case isString:
		base = "text"
	case isNumber:
		base = "number"
	case basic != nil && basic.Info()&types.IsBoolean != 0:
		base = "column"
		f.constructor = "BoolOf"
	default:
		if s, ok := f.stored.Underlying().(*types.Slice); ok && types.Identical(s.Elem(), types.Typ[types.Byte]) {
			base = "column"
			f.constructor = "BytesOf"
			f.bytes = true
		}
	}
	if len(tag.Values) != 0 && modelmeta.FieldEnum(f.stored, func(named *types.Named) bool { return p.enumTypes[named] || hasEnumDescriptor(named) }) == nil {
		return fail("values applies only to an enum field or a collection of enums")
	}
	if f.slot {
		if f.nullable {
			return fail("Nullable does not apply to a slot")
		}
		if relation && tag.Slot {
			return fail("slot conflicts with relation type")
		}
		if tag.Slot && n != nil && (named(framework+"/translations", "Text") || named(framework+"/metadata", "Value") || named(framework+"/attachments", "One") || named(framework+"/attachments", "Many")) {
			return fail("slot is inferred from the field type; remove it")
		}
		for _, opt := range []struct{ name, value string }{{"column", tag.Column}, {"role", tag.Role}, {"input", tag.Input}, {"codec", tag.Codec}, {"key-role", tag.KeyRole}} {
			if opt.value != "" {
				return fail(opt.name + " does not apply to a slot")
			}
		}
		if tag.DatabaseDefault {
			return fail("default does not apply to a slot")
		}
		if f.key {
			return fail("unsupported primary key type " + field.Type().String())
		}
		return f, nil
	}
	if tag.Column == modelmeta.Snake(field.Name()) {
		return fail("column=" + tag.Column + " is redundant; remove it")
	}
	if f.key {
		f.args = []types.Type{f.stored}
		if f.nullable || encrypted || password && tag.Codec == "" || !types.Comparable(field.Type()) || !modelKeyType(f.stored, tag.Codec != "") {
			return fail("unsupported primary key type " + field.Type().String())
		}
		if named(framework+"/model", "ID") && !types.Identical(n.TypeArgs().At(0), d.typ) {
			return fail("primary model.ID must belong to this model")
		}
		if tag.Role != "" {
			return fail("role applies only to non-key fields; remove it")
		}
		if tag.KeyRole != "" && f.member == "" {
			return fail("key-role requires member")
		}
		inferred := "lookup"
		if isString {
			inferred = "text"
		} else if isNumber {
			inferred = "number"
		}
		if tag.KeyRole == inferred {
			return fail("key-role=" + tag.KeyRole + " is inferred from the key type; remove it")
		}
		role := tag.KeyRole
		if role == "" {
			role = inferred
		}
		f.tag.KeyRole = role
		switch role {
		case "lookup":
			f.carrier = "Key"
		case "primary":
			f.carrier = "Primary"
		case "input":
			f.carrier = "InputKey"
			if tag.Input == "" {
				return fail("key-role=input requires input")
			}
		case "number", "primary-number":
			if !isNumber {
				return fail("key-role=" + role + " does not apply to " + field.Type().String())
			}
			f.carrier = "NumberLookupKey"
			if role == "primary-number" {
				f.carrier = "NumberPrimary"
			}
		case "text", "primary-text":
			if !isString {
				return fail("key-role=" + role + " does not apply to " + field.Type().String())
			}
			f.carrier = "TextLookupKey"
			if role == "primary-text" {
				f.carrier = "TextPrimary"
			}
		}
		if tag.Input != "" && role != "input" {
			return fail("input on a primary key requires key-role=input and an exposed key member")
		}
	} else {
		if tag.KeyRole != "" {
			return fail("key-role applies only to the primary key")
		}
		if tag.Codec != "" && tag.Role != "" {
			if tag.Role == "column" {
				return fail("role=column is implied by codec; remove it")
			}
			return fail("codec selects a plain column; remove role=" + tag.Role)
		}
		if decimalType && tag.Codec == "" && tag.Role != "column" && tag.Role != "number" {
			return fail("decimal field requires role=column or role=number")
		}
		role := tag.Role
		timeDecision := base == "time" && ((field.Name() == "CreatedAt" || field.Name() == "UpdatedAt") && !d.noTimestamps || field.Name() == "DeletedAt" && !d.noSoftDeletes)
		plainDefault := f.nullable && f.constructor == "BoolOf" || tag.Input != "" && base == "column" && !f.bytes && !password
		if role != "" && role == base && !decimalType && !(role == "column" && !plainDefault) && !(role == "time" && timeDecision) {
			return fail("role=" + role + " is inferred from the field type; remove it")
		}
		if role == "" {
			role = base
		}
		if tag.Codec != "" {
			role = "column"
		}
		allowed := role == base || role == "column" && !encrypted
		if tag.Codec != "" && !encrypted {
			allowed = true
		}
		if role == "created-at" || role == "updated-at" {
			allowed = instant && !f.nullable
		}
		if role == "soft-delete" {
			allowed = instant && f.nullable
		}
		if encrypted && tag.Codec != "" {
			return fail("encrypted fields require their envelope constructor; codec is not supported")
		}
		if !allowed {
			return fail("role=" + role + " does not apply to " + field.Type().String())
		}
		if base == "" && tag.Codec == "" {
			return fail("unsupported stored type " + field.Type().String() + "; declare codec")
		}

		if tag.Codec == "" && role == "column" {
			if password && tag.Input != "" {
				return fail("password input requires codec")
			}
			family := ""
			switch {
			case password && tag.Role == "column":
				family = "password.Hash"
			case base == "json":
				family = "value.JSON"
			case f.bytes && tag.Role == "column":
				family = "bytes"
			}
			if family != "" {
				return fail("role=column requires codec for " + family + "; declare codec=<name> without role")
			}
		}

		if role == "created-at" || role == "updated-at" || role == "soft-delete" {
			if tag.DatabaseDefault {
				return fail("managed timestamp roles cannot use default=database")
			}
		}
		carriers := map[string]string{"column": "Column", "text": "Text", "number": "Number", "enum": "Enum", "json": "JSON", "time": "Time", "interval": "Interval", "created-at": "CreatedAt", "updated-at": "UpdatedAt", "soft-delete": "SoftDelete", "encrypted": "Encrypted"}
		f.carrier = carriers[role]
		if role != base || tag.Codec != "" || tag.Role == "column" {
			f.constructor = ""
			f.args = []types.Type{f.stored}
		}
		f.intervalColumn = role == "column" && base == "interval" && tag.Codec == ""

		if f.constructor == "" {
			f.constructor = f.carrier + "Of"
		}
		if f.nullable && role != "soft-delete" {
			f.carrier = "Nullable" + f.carrier
			if f.constructor == "BoolOf" {
				f.constructor = "ColumnOf"
			}
			f.constructor = "Nullable" + f.constructor
		}
	}
	if tag.Input != "" {
		if encrypted || strings.Contains(f.carrier, "CreatedAt") || strings.Contains(f.carrier, "UpdatedAt") || f.carrier == "SoftDelete" {
			return fail("input does not apply to " + f.carrier)
		}
		evaluated, err := types.Eval(p.fset, p.types, field.Pos(), tag.Input)
		if err != nil || !evaluated.IsType() {
			return fail("input requires a Go type")
		}
		f.input = evaluated.Type
		if !modelMutatorInput(f.input) {
			return fail("mutator input must be concrete and non-null")
		}
		if !f.key {
			prefix := ""
			family := f.carrier
			if f.nullable {
				prefix = "Nullable"
				family = strings.TrimPrefix(family, "Nullable")
			}
			f.carrier = prefix + "Input" + family
			f.constructor = f.carrier + "Of"
			if f.bytes && tag.Codec == "" && tag.Role != "column" {
				f.constructor = prefix + "InputBytesOf"
			}
		}
		method, _, _ := types.LookupFieldOrMethod(types.NewPointer(d.typ), true, p.types, "Mutate"+field.Name())
		if tag.Mutate == "Mutate"+field.Name() && method != nil {
			return fail("mutate names the conventional method; remove it")
		}
		if tag.Mutate != "" {
			if method != nil {
				return fail("mutate conflicts with Mutate" + field.Name())
			}
			fn, err := modelFunction(p, tag.Mutate, "mutate")
			if err != nil {
				return f, err
			}
			if !modelTransform(fn, f.input, f.stored) {
				return fail("mutate has an incompatible signature")
			}
		} else {
			fn, ok := method.(*types.Func)
			if !ok {
				return fail("input requires Mutate" + field.Name() + " or mutate")
			}
			if !modelTransform(fn, f.input, f.stored) {
				return fail("Mutate" + field.Name() + " has an incompatible signature")
			}
			if _, pointer := fn.Type().(*types.Signature).Recv().Type().(*types.Pointer); pointer {
				return fail("model field method " + fn.Name() + " requires a value receiver")
			}
			f.method = fn
		}
	} else if _, exists := p.methods[d.name]["Mutate"+field.Name()]; exists {
		return fail("Mutate" + field.Name() + " requires input")
	}
	if tag.Codec != "" {
		if encrypted {
			return fail("codec does not apply to " + f.carrier)
		}
		fn, err := modelFunction(p, tag.Codec, "codec")
		if err != nil {
			return f, err
		}
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			return fail("codec has an incompatible signature")
		}
		codecType, _ := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
		if codecType == nil || !isNamed(codecType, framework+"/database/codec", "Codec") || codecType.TypeArgs().Len() != 1 || !types.Identical(codecType.TypeArgs().At(0), f.stored) {
			return fail("codec has an incompatible signature")
		}
	}
	return f, nil
}

func modelTransform(fn *types.Func, input, stored types.Type) bool {
	sig := fn.Type().(*types.Signature)
	return !sig.Variadic() && sig.TypeParams().Len() == 0 && sig.Params().Len() == 1 && sig.Results().Len() == 2 && types.Identical(sig.Params().At(0).Type(), input) && types.Identical(sig.Results().At(0).Type(), stored) && types.Identical(sig.Results().At(1).Type(), types.Universe.Lookup("error").Type())
}

// modelFieldType uses the runtime's concrete carrier aliases; input and relation
// arguments remain Go types, never generated name-based dispatch.
func modelFieldType(e *emitter, orm string, d modelDeclaration, f modelField) string {
	args := []string{d.name}
	if f.input != nil {
		args = append(args, e.typeName(f.input))
	}
	for _, arg := range f.args {
		args = append(args, e.typeName(arg))
	}
	return fmt.Sprintf("%s.%s[%s]", orm, f.carrier, strings.Join(args, ", "))
}
