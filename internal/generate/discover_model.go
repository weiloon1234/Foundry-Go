package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

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
	return safeerror.New(fmt.Sprintf("%s: model %s: %s", p.fset.Position(pos), model, message))
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
			return fail("unknown directive option " + key)
		}
	}
	if !sqlname.Table(d.table) {
		return fail("table requires an explicit SQL identifier")
	}
	if !modelmeta.ModelIdentifier(d.variable) {
		return fail("var requires a Go identifier")
	}
	if !modelmeta.ModelIdentifier(d.key) {
		return fail("key requires a struct field name")
	}
	if args["identity"] == "true" {
		return fail("identity=true is redundant; remove it")
	}
	if args["identity"] != "" && args["identity"] != "false" {
		return fail("identity requires false to opt out")
	}
	d.identityVar = modelmeta.IdentityVariable(d.variable)
	if args["timestamps"] != "" && !d.noTimestamps {
		return fail("timestamps requires none")
	}
	if args["softdeletes"] != "" && !d.noSoftDeletes {
		return fail("softdeletes requires none")
	}
	var ok bool
	d.shape, ok = named.Underlying().(*types.Struct)
	if !ok {
		return fail("requires a struct")
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
				return fail("declare only one soft-delete field")
			}
			if d.noSoftDeletes {
				return fail("softdeletes=none conflicts with soft-delete")
			}
			d.hasSoft = true
		}
		if d.noTimestamps && (f.carrier == "CreatedAt" || f.carrier == "UpdatedAt") {
			return fail("timestamps=none conflicts with managed timestamps")
		}
		d.fields = append(d.fields, f)
		if f.member != "" {
			if members[f.member] {
				return fail("duplicate table member " + f.member)
			}
			members[f.member] = true
			d.members = append(d.members, f.member)
		}
	}
	if !keyFound {
		return fail("key field " + d.key + " does not exist or is ignored")
	}
	if d.hasSoft {
		if members["Soft"] {
			return fail("duplicate table member Soft")
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
			return fail(option.name + " has an incompatible signature")
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
	return nil, safeerror.New(option + " requires a named function")
}
