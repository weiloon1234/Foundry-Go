package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/config"
)

type configDeclaration struct {
	name     string
	typ      *types.Named
	fields   []configField
	position token.Position
}

type configField struct {
	name, path, key, codec string
	typ                    types.Type
	secret                 bool
	children               []configField
}

func discoverConfig(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (configDeclaration, error) {
	d := configDeclaration{name: spec.Name.Name, typ: named, position: p.fset.Position(spec.Pos())}
	if len(args) != 0 {
		return d, p.diagnostic(spec.Pos(), "config declarations do not accept options")
	}
	if _, ok := named.Underlying().(*types.Struct); !ok {
		return d, p.diagnostic(spec.Pos(), "config declaration must be a struct")
	}
	return d, nil
}

func resolveConfig(p *packageInput, declaration *configDeclaration) error {
	var err error
	declaration.fields, err = configFields(p, declaration.typ, "", "", false, make(map[types.Type]bool), 0)
	if err != nil {
		return err
	}
	// Schema construction owns name, namespace and environment collision rules.
	// Probe those rules without creating a second implementation in the generator.
	type probe struct{ Value string }
	var fields []config.Field[probe]
	walkConfigLeaves(declaration.fields, func(field configField) {
		fields = append(fields, config.String(field.key, func(v *probe) *string { return &v.Value }))
	})
	if _, err := config.New(fields...); err != nil {
		return fmt.Errorf("%s: %w", declaration.position, err)
	}
	return nil
}

func configFields(p *packageInput, typ types.Type, path, prefix string, sensitive bool, ancestors map[types.Type]bool, depth int) ([]configField, error) {
	if depth > 32 || ancestors[typ] {
		return nil, fmt.Errorf("config groups cannot be recursive or exceed 32 levels")
	}
	ancestors[typ] = true
	defer delete(ancestors, typ)
	structure := typ.Underlying().(*types.Struct)
	var fields []configField
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		fail := func(message string) ([]configField, error) { return nil, p.diagnostic(field.Pos(), message) }
		tags, err := parseTags(structure.Tag(i))
		if err != nil {
			return fail(err.Error())
		}
		raw, explicit := tags["config"]
		if raw == "-" {
			continue
		}
		if field.Embedded() || !field.Exported() {
			return fail("config fields must be exported and non-embedded; use config:\"-\" to skip")
		}
		parts := strings.Split(raw, ",")
		name := parts[0]
		if !explicit || name == "" {
			name = snake(field.Name())
		}
		if err := config.ValidateNamespace(name); err != nil {
			return fail("config tag requires a valid setting name")
		}
		options := make(map[string]bool)
		for _, option := range parts[1:] {
			if (option != "secret" && option != "json") || options[option] {
				return fail("unknown or repeated config tag option")
			}
			options[option] = true
		}
		node := configField{name: field.Name(), path: path + field.Name(), key: prefix + name, typ: field.Type(), secret: sensitive || options["secret"]}
		base := types.Unalias(field.Type())
		if named, ok := base.(*types.Named); ok {
			switch {
			case isNamed(named, "time", "Duration"):
				node.codec = "Duration"
			case isNamed(named, framework+"/secret", "String"):
				node.codec, node.secret = "Secret", true
			case p.enumTypes[named] || hasEnumDescriptor(named):
				node.codec = "Enum"
			}
		}
		_, unmarshal, invalidText := urlTextMethods(base)
		if node.codec == "" && !options["json"] && (unmarshal || invalidText) {
			if invalidText {
				return fail("config text decoder must implement encoding.TextUnmarshaler")
			}
			node.codec = "Text"
		}
		if options["json"] {
			if node.codec != "" {
				return fail("config json option cannot replace a duration, secret or enum codec")
			}
			node.codec = "JSON"
		}
		if node.codec == "" {
			switch underlying := base.Underlying().(type) {
			case *types.Struct:
				node.children, err = configFields(p, base, node.path+".", node.key+".", node.secret, ancestors, depth+1)
				if err != nil {
					return fail(err.Error())
				}
				if len(node.children) == 0 {
					return fail("config group must contain at least one setting")
				}
			case *types.Basic:
				if underlying.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) == 0 || underlying.Kind() == types.Uintptr || underlying.Info()&types.IsUntyped != 0 {
					return fail("unsupported config scalar type")
				}
				node.codec = "Scalar"
			case *types.Slice, *types.Array, *types.Map:
				node.codec = "JSON"
			default:
				return fail("unsupported config field; use a concrete value or supported text codec")
			}
		}
		if node.codec == "JSON" {
			secret, err := configJSONType(base, make(map[types.Type]bool), 0)
			if err != nil {
				return fail(err.Error())
			}
			node.secret = node.secret || secret
		}
		fields = append(fields, node)
	}
	return fields, nil
}

// JSON leaves keep the existing codec, but their Go shape must still be concrete.
// Sensitive contents mark the whole structured key's provenance as sensitive.
func configJSONType(typ types.Type, seen map[types.Type]bool, depth int) (bool, error) {
	base := types.Unalias(typ)
	if named, ok := base.(*types.Named); ok && isNamed(named, framework+"/secret", "String") {
		return true, nil
	}
	// netip.Prefix is an immutable native CIDR value with a standard text/JSON
	// representation, not a user struct whose hidden fields need discovery.
	if named, ok := base.(*types.Named); ok && isNamed(named, "net/netip", "Prefix") {
		return false, nil
	}
	if seen[base] {
		return false, nil
	}
	if depth > 64 {
		return false, fmt.Errorf("structured config type exceeds 64 levels")
	}
	seen[base] = true
	defer delete(seen, base)
	switch value := base.Underlying().(type) {
	case *types.Basic:
		if value.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) != 0 && value.Kind() != types.Uintptr {
			return false, nil
		}
	case *types.Pointer:
		return configJSONType(value.Elem(), seen, depth+1)
	case *types.Array:
		return configJSONType(value.Elem(), seen, depth+1)
	case *types.Slice:
		return configJSONType(value.Elem(), seen, depth+1)
	case *types.Map:
		key, ok := value.Key().Underlying().(*types.Basic)
		if !ok || key.Kind() != types.String {
			return false, fmt.Errorf("structured config maps require string keys")
		}
		return configJSONType(value.Elem(), seen, depth+1)
	case *types.Struct:
		secret := false
		for i := 0; i < value.NumFields(); i++ {
			tags, err := parseTags(value.Tag(i))
			if err != nil {
				return false, err
			}
			if tags["json"] == "-" {
				continue
			}
			if !value.Field(i).Exported() || value.Field(i).Embedded() {
				return false, fmt.Errorf("structured config requires exported non-embedded fields")
			}
			sensitive, err := configJSONType(value.Field(i).Type(), seen, depth+1)
			if err != nil {
				return false, err
			}
			secret = secret || sensitive || strings.Contains(tags["config"], ",secret")
		}
		return secret, nil
	}
	return false, fmt.Errorf("unsupported structured config type")
}

func walkConfigLeaves(fields []configField, visit func(configField)) {
	for _, field := range fields {
		if len(field.children) == 0 {
			visit(field)
		} else {
			walkConfigLeaves(field.children, visit)
		}
	}
}

func configKeySetName(owner, path string) string {
	return owner + strings.ReplaceAll(path, ".", "") + "ConfigKeySet"
}

func configSymbols(declaration configDeclaration) []string {
	result := []string{declaration.name + "ConfigKeys", declaration.name + "ConfigSchema", configKeySetName(declaration.name, "")}
	var groups func([]configField)
	groups = func(fields []configField) {
		for _, field := range fields {
			if len(field.children) > 0 {
				result = append(result, configKeySetName(declaration.name, field.path))
				groups(field.children)
			}
		}
	}
	groups(declaration.fields)
	return result
}
