package generate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type modelDeclaration struct {
	name, table, variable, tableType, key, identityVar string
	identity, noTimestamps, noSoftDeletes, hasSoft     bool
	hooks, retrieval, scopes                           string
	typ                                                *types.Named
	shape                                              *types.Struct
	fields                                             []modelField
	members                                            []string
	position                                           token.Position
}
type modelField struct {
	help                                       fieldHelp
	field                                      *types.Var
	tag                                        modelmeta.ModelTag
	member, carrier, constructor               string
	args                                       []types.Type
	stored, input                              types.Type
	nullable, key, slot, bytes, intervalColumn bool
	method                                     *types.Func
}

func modelError(p *packageInput, pos token.Pos, model, field, message string) error {
	if field != "" {
		model += "." + field
	}
	position := p.fset.Position(pos)
	return diagnostic.GenerateDecl.At(position.Filename, position.Line, position.Column).Text("model "+model, message).Err()
}
func discoverModel(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (modelDeclaration, error) {
	d := modelDeclaration{name: spec.Name.Name, table: args["table"], variable: args["var"], tableType: spec.Name.Name + "Table", key: args["key"], identity: args["identity"] != "false", noTimestamps: args["timestamps"] == "none", noSoftDeletes: args["softdeletes"] == "none", hooks: args["hooks"], retrieval: args["retrieval"], scopes: args["scopes"], typ: named, position: p.fset.Position(spec.Pos())}
	fail := func(message string) (modelDeclaration, error) {
		return d, modelError(p, spec.Pos(), d.name, "", message)
	}
	for _, key := range sortedNames(args) {
		switch key {
		case "table", "var", "key", "identity", "timestamps", "softdeletes", "hooks", "retrieval", "scopes":
		default:
			return fail("unknown directive option " + key + "; the options are table, var, key, identity, timestamps, softdeletes, hooks, retrieval and scopes")
		}
	}
	if !sqlname.Table(d.table) {
		return fail("table requires an explicit SQL identifier; write table=<name> or table=<schema>.<name>")
	}
	if !modelmeta.ModelIdentifier(d.variable) {
		return fail("var requires a Go identifier; write var=<Name>")
	}
	if !modelmeta.ModelIdentifier(d.key) {
		return fail("key requires a struct field name; write key=<Field> naming the primary-key field")
	}
	if args["identity"] == "true" {
		return fail("identity=true is redundant; remove it")
	}
	if args["identity"] != "" && args["identity"] != "false" {
		return fail("identity requires false to opt out; write identity=false or remove the option")
	}
	d.identityVar = modelmeta.IdentityVariable(d.variable)
	if args["timestamps"] != "" && !d.noTimestamps {
		return fail("timestamps requires none; write timestamps=none or remove the option")
	}
	if args["softdeletes"] != "" && !d.noSoftDeletes {
		return fail("softdeletes requires none; write softdeletes=none or remove the option")
	}
	var ok bool
	d.shape, ok = named.Underlying().(*types.Struct)
	if !ok {
		return fail("requires a struct; declare the model as a struct type")
	}
	reader := p.sourceDocumentation()
	source, err := reader.structure(named)
	if err != nil {
		return d, err
	}
	members := map[string]bool{"Table": true}
	d.members = []string{"Table"}
	keyFound := false
	for i := 0; i < d.shape.NumFields(); i++ {
		field := d.shape.Field(i)
		tag, err := modelmeta.ParseModelTag(d.shape.Tag(i))
		if err != nil {
			return d, modelError(p, field.Pos(), d.name, field.Name(), err.Error())
		}
		if field.Embedded() {
			return d, modelError(p, field.Pos(), d.name, field.Name(), "embedded fields are not supported; declare each model field directly")
		}
		if tag.Ignore {
			continue
		}
		f, err := discoverModelField(p, &d, field, tag)
		if err != nil {
			return d, modelError(p, field.Pos(), d.name, field.Name(), err.Error())
		}
		f.help, err = reader.help(source, field.Name())
		if err != nil {
			return d, err
		}
		if f.key {
			keyFound = true
		}
		if f.carrier == "SoftDelete" {
			if d.hasSoft {
				return fail("declare only one soft-delete field; remove the others")
			}
			if d.noSoftDeletes {
				return fail("softdeletes=none conflicts with soft-delete; remove softdeletes=none or the soft-delete field")
			}
			d.hasSoft = true
		}
		if d.noTimestamps && (f.carrier == "CreatedAt" || f.carrier == "UpdatedAt") {
			return fail("timestamps=none conflicts with managed timestamps; remove timestamps=none or the managed timestamp fields")
		}
		d.fields = append(d.fields, f)
		if f.member != "" {
			if members[f.member] {
				return fail("duplicate table member " + f.member + "; give one field member=<Name> with a distinct name")
			}
			members[f.member] = true
			d.members = append(d.members, f.member)
		}
	}
	if !keyFound {
		return fail("key field " + d.key + " does not exist or is ignored; name an exported persisted field in key=")
	}
	if d.hasSoft {
		if members["Soft"] {
			return fail("duplicate table member Soft; give the field named Soft member=<Name> with a distinct name")
		}
		d.members = append(d.members, "Soft")
	}
	if err := validateModel(p, &d); err != nil {
		return d, err
	}
	for _, option := range []struct{ name, value, result string }{{"hooks", d.hooks, "Hooks"}, {"retrieval", d.retrieval, "RetrievalHooks"}, {"scopes", d.scopes, "GlobalScope"}} {
		if option.value == "" {
			continue
		}
		fn, err := modelFunction(p, option.value, option.name)
		if err != nil {
			return fail(err.Error())
		}
		sig := fn.Type().(*types.Signature)
		valid := sig.Results().Len() == 1
		if option.name == "scopes" {
			valid = valid && sig.Params().Len() == 1
			if valid {
				t, _ := types.Unalias(sig.Params().At(0).Type()).(*types.Named)
				valid = t != nil && t.Obj().Pkg() == p.types && t.Obj().Name() == d.tableType
			}
		} else {
			valid = valid && sig.Params().Len() == 0
		}
		if valid {
			t := sig.Results().At(0).Type()
			if option.name == "scopes" {
				s, ok := t.(*types.Slice)
				valid = ok
				if ok {
					t = s.Elem()
				}
			}
			n, _ := types.Unalias(t).(*types.Named)
			valid = valid && n != nil && isNamed(n, framework+"/database/orm", option.result) && n.TypeArgs().Len() == 1 && types.Identical(n.TypeArgs().At(0), named)
		}
		if !valid {
			shape := "func() orm." + option.result + "[" + d.name + "]"
			if option.name == "scopes" {
				shape = "func(" + d.tableType + ") []orm." + option.result + "[" + d.name + "]"
			}
			return fail(option.name + " has an incompatible signature; declare " + shape)
		}
	}
	return d, nil
}

func modelFunction(p *packageInput, name, option string) (*types.Func, error) {
	if modelmeta.ModelIdentifier(name) {
		fn, ok := p.types.Scope().Lookup(name).(*types.Func)
		if ok {
			sig := fn.Type().(*types.Signature)
			if sig.TypeParams().Len() == 0 && !sig.Variadic() {
				return fn, nil
			}
		}
	}
	return nil, safeerror.New(option + " requires a named function; name a package-level function that is not generic or variadic")
}
