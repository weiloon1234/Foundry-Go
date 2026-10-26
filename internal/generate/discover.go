package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
)

type enum struct {
	name   string
	labels string
	// trimType derives label keys without the enum type's name, which
	// type-prefixed constants such as StatusDraft repeat.
	trimType bool
	typ      *types.Named
	base     *types.Basic
	values   []enumValue
	position token.Position
}
type enumValue struct {
	name, literal string
	position      token.Position
}
type metadata struct {
	enums []enum

	paths     []pathDeclaration
	multipart []multipartDeclaration
	queries   []queryDeclaration
	dtos      []dtoDeclaration
	configs   []configDeclaration
	unions    []unionDeclaration
}

func discover(p *packageInput) (metadata, error) {
	defer func() { p.documentation = nil }()
	var result metadata
	for _, file := range p.files {
		for _, decl := range file.syntax.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				comments := typeSpec.Doc
				if comments == nil {
					comments = gen.Doc
				}
				kind, args, err := directive(comments)
				if err != nil {
					return result, p.diagnostic(typeSpec.Pos(), err.Error())
				}
				if kind == "" {
					continue
				}
				if comments == gen.Doc && len(gen.Specs) > 1 {
					return result, p.diagnostic(gen.Pos(), "Foundry directives on grouped declarations must attach to one type")
				}
				object, ok := p.types.Scope().Lookup(typeSpec.Name.Name).(*types.TypeName)
				if !ok || object.IsAlias() || !object.Exported() {
					return result, p.diagnostic(typeSpec.Pos(), "Foundry declarations require an exported defined type")
				}
				named, ok := object.Type().(*types.Named)
				if !ok || (named.TypeParams().Len() != 0 && kind != "dto") {
					return result, p.diagnostic(typeSpec.Pos(), "generic Foundry declarations are not supported")
				}
				switch kind {
				case "union":
					declaration, err := discoverUnion(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.unions = append(result.unions, declaration)
				case "config":
					declaration, err := discoverConfig(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.configs = append(result.configs, declaration)
				case "message":
					declaration, err := discoverMessage(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.dtos = append(result.dtos, declaration)
				case "dto":
					declaration, err := discoverDTO(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.dtos = append(result.dtos, declaration)
				case "multipart":
					declaration, err := discoverMultipart(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.multipart = append(result.multipart, declaration)
				case "query", "form":
					declaration, err := discoverQuery(p, typeSpec, named, args, kind)
					if err != nil {
						return result, err
					}
					result.queries = append(result.queries, declaration)
				case "path":
					declaration, err := discoverPath(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.paths = append(result.paths, declaration)
				case "enum":
					e, err := discoverEnum(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.enums = append(result.enums, e)
				}
			}
		}
	}
	// Enum membership fields expose equality rather than free-form text operators.
	enums := make(map[*types.Named]bool)
	for _, e := range result.enums {
		enums[e.typ] = true
	}
	p.enumTypes = enums
	// Transport hints are checked once enum declarations are known.
	for _, declaration := range result.paths {
		for _, field := range declaration.fields {
			if err := p.transportPresentation(field.presentation, field.typ); err != nil {
				return result, p.diagnostic(field.position, err.Error())
			}
		}
	}
	for _, declaration := range result.queries {
		for _, field := range declaration.fields {
			if err := p.transportPresentation(field.presentation, field.typ); err != nil {
				return result, p.diagnostic(field.position, err.Error())
			}
		}
	}
	for _, declaration := range result.multipart {
		for _, field := range declaration.fields {
			if err := p.transportPresentation(field.presentation, field.typ); err != nil {
				return result, p.diagnostic(field.position, err.Error())
			}
		}
	}
	p.unionTypes = make(map[*types.Named]*unionDeclaration, len(result.unions))
	for i := range result.unions {
		declaration := &result.unions[i]
		p.unionTypes[declaration.typ] = declaration
	}
	names := make(map[string]bool)
	for i := range result.paths {
		declaration := &result.paths[i]
		if err := resolvePathCodecs(p, declaration); err != nil {
			return result, err
		}
		symbol := declaration.name + "Descriptor"
		if names[symbol] {
			return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
		}
		names[symbol] = true
	}
	for i := range result.queries {
		declaration := &result.queries[i]
		if err := resolveQueryCodecs(p, declaration); err != nil {
			return result, err
		}
		suffixes := []string{"Descriptor"}
		if declaration.source == "form" {
			var err error
			declaration.fieldHelp, err = p.validationHelp(declaration.typ, declaration.validationProperties())
			if err != nil {
				return result, err
			}
			suffixes = append(suffixes, "ValidationFields", "ValidationFieldSet")
		}
		for _, suffix := range suffixes {
			symbol := declaration.name + suffix
			if names[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
			}
			names[symbol] = true
		}
	}
	for i := range result.unions {
		declaration := &result.unions[i]
		if err := resolveJSONGraph(p, &declaration.dtoGraph, declaration.typ, declaration.typ.Obj().Pos(), true); err != nil {
			return result, err
		}
		for _, symbol := range unionSymbols(*declaration) {
			if names[symbol] || p.defined[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
			}
			names[symbol] = true
		}
	}
	for i := range result.multipart {
		declaration := &result.multipart[i]
		if err := resolveMultipartFields(p, declaration); err != nil {
			return result, err
		}
		var err error
		declaration.fieldHelp, err = p.validationHelp(declaration.typ, declaration.validationProperties())
		if err != nil {
			return result, err
		}
		for _, suffix := range []string{"Descriptor", "ValidationFields", "ValidationFieldSet"} {
			symbol := declaration.name + suffix
			if names[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
			}
			names[symbol] = true
		}
	}
	for i := range result.dtos {
		declaration := &result.dtos[i]
		if err := resolveDTOSchema(p, declaration); err != nil {
			return result, err
		}
		suffixes := []string{"JSON"}
		if declaration.role != dtoResponseRole {
			suffixes = append(suffixes, "ValidationFields", "ValidationFieldSet")
		}
		if declaration.message != nil {
			if err := validateMessageSchema(p, *declaration); err != nil {
				return result, err
			}
			suffixes = append(suffixes, "Message")
		}
		for _, suffix := range suffixes {
			symbol := declaration.name + suffix
			if names[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
			}
			names[symbol] = true
		}
	}

	for i := range result.configs {
		declaration := &result.configs[i]
		if err := resolveConfig(p, declaration); err != nil {
			return result, err
		}
		for _, symbol := range configSymbols(*declaration) {
			if names[symbol] || p.defined[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", declaration.position, symbol)
			}
			names[symbol] = true
		}
	}
	sort.Slice(result.unions, func(i, j int) bool { return result.unions[i].name < result.unions[j].name })
	sort.Slice(result.configs, func(i, j int) bool { return result.configs[i].name < result.configs[j].name })
	sort.Slice(result.multipart, func(i, j int) bool { return result.multipart[i].name < result.multipart[j].name })
	sort.Slice(result.dtos, func(i, j int) bool { return result.dtos[i].name < result.dtos[j].name })
	sort.Slice(result.paths, func(i, j int) bool { return result.paths[i].name < result.paths[j].name })
	sort.Slice(result.queries, func(i, j int) bool { return result.queries[i].name < result.queries[j].name })
	sort.Slice(result.enums, func(i, j int) bool { return result.enums[i].name < result.enums[j].name })
	return result, nil
}

// Imported enum metadata is an ordinary typed method, so consumers and gopls
// see the same contract. A matching name alone is not sufficient.
func hasEnumDescriptor(named *types.Named) bool {
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if method.Name() != "EnumDescriptor" {
			continue
		}
		sig, ok := method.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		result, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
		if ok && isNamed(result, framework+"/enum", "Descriptor") && result.TypeArgs().Len() == 1 && types.Identical(result.TypeArgs().At(0), named) {
			return true
		}
	}
	return false
}

func directive(group *ast.CommentGroup) (string, map[string]string, error) {
	kind := ""
	args := make(map[string]string)
	if group == nil {
		return kind, args, nil
	}
	for _, comment := range group.List {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
		if !strings.HasPrefix(text, "foundry:") {
			continue
		}
		if kind != "" {
			return "", nil, fmt.Errorf("duplicate Foundry declaration directive")
		}
		words := strings.Fields(strings.TrimPrefix(text, "foundry:"))
		if len(words) == 0 {
			return "", nil, fmt.Errorf("missing Foundry directive kind")
		}
		kind = words[0]
		if kind == "model" || kind == "projection" {
			return "", nil, fmt.Errorf("foundry:%s generation is retired; use handwritten orm.Define or rows.Define declarations", kind)
		}
		if kind != "enum" && kind != "path" && kind != "dto" && kind != "query" && kind != "form" && kind != "multipart" && kind != "message" && kind != "config" && kind != "union" {
			return "", nil, fmt.Errorf("unsupported Foundry declaration directive")
		}
		for _, word := range words[1:] {
			key, value, ok := strings.Cut(word, "=")
			if !ok || value == "" {
				return "", nil, fmt.Errorf("directive options require key=value")
			}
			if _, exists := args[key]; exists {
				return "", nil, fmt.Errorf("duplicate directive option %s", key)
			}
			args[key] = value
		}
	}
	return kind, args, nil
}

func isNamed(named *types.Named, path, name string) bool {
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == path && named.Obj().Name() == name
}
func (p *packageInput) diagnostic(pos token.Pos, message string) error {
	return fmt.Errorf("%s: %s", p.fset.Position(pos), message)
}

// diagnosticAt reports at an already-resolved field position.
func (p *packageInput) diagnosticAt(position token.Position, message string) error {
	return fmt.Errorf("%s: %s", position, message)
}

// Reject malformed/duplicate tags instead of letting reflect.StructTag.Lookup
// silently hide a misspelled persistence declaration.
func parseTags(text string) (map[string]string, error) { return modelmeta.ParseTags(text) }

func snake(name string) string { return modelmeta.Snake(name) }

func exportedName(name string) string {
	var out strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part != "" {
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			out.WriteString(string(runes))
		}
	}
	return out.String()
}

// checkEncryptedFields keeps encrypted values bound to a key known before the
// insert, and leaves their plaintext untransformed: a mutator would run on an
// envelope boundary the framework owns.
