package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type model struct {
	name, table, query string
	hooks              string
	retrieval          string
	timestamps         [2]string
	softDelete         string
	typ                *types.Named
	fields             []field
	relations          []relationField
	aggregates         []aggregateField
	extensions         []extensionSlot
	extensionOwner     string
	extensionPolicy    bool
	position           token.Position
}
type relationField struct {
	name, kind string
	target     types.Type
	pivot      types.Type
}
type aggregateField struct {
	name string
	typ  types.Type
}
type field struct {
	name, column, kind      string
	typ, base               types.Type
	nullable, primary, enum bool
	databaseDefault         bool
	input                   types.Type // Non-nil only when mutation input differs from stored base.
	mutator                 string
	accessor                string
	timestamp               string
	softDelete              bool
	position                token.Position
}
type enum struct {
	name     string
	labels   string
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
	models      []model
	enums       []enum
	projections []projection
	paths       []pathDeclaration
	multipart   []multipartDeclaration
	queries     []queryDeclaration
	dtos        []dtoDeclaration
	configs     []configDeclaration
	unions      []unionDeclaration
}

func discover(p *packageInput) (metadata, error) {
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
				case "projection":
					projection, err := discoverProjection(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.projections = append(result.projections, projection)
					if projection.dto {
						declaration, err := discoverDTO(p, typeSpec, named, nil)
						if err != nil {
							return result, err
						}
						declaration.projected = true
						result.dtos = append(result.dtos, declaration)
					}
				case "model":
					m, err := discoverModel(p, typeSpec, named, args)
					if err != nil {
						return result, err
					}
					result.models = append(result.models, m)
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
	p.unionTypes = make(map[*types.Named]*unionDeclaration, len(result.unions))
	for i := range result.unions {
		declaration := &result.unions[i]
		p.unionTypes[declaration.typ] = declaration
	}
	names, tables, owners := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for i := range result.models {
		m := &result.models[i]
		if tables[m.table] {
			return result, fmt.Errorf("%s: duplicate model table %s", m.position, m.table)
		}
		tables[m.table] = true
		if len(m.extensions) > 0 {
			if owners[m.extensionOwner] {
				return result, fmt.Errorf("%s: duplicate extension owner %s; declare a distinct extension_owner", m.position, m.extensionOwner)
			}
			owners[m.extensionOwner] = true
		}
		markEnumFields(m.fields, enums)
		symbols := []string{m.name + "FieldSet", m.name + "Fields", m.name + "ScopedFieldSet", m.name + "NullableFieldSet", m.name + "FieldsAt", m.name + "NullableFieldsAt", m.name + "Draft", m.name + "UpdateFromBuilder", "Update" + m.name + "From", m.name + "DeleteUsingBuilder", "Delete" + m.name + "Using", m.name + "InsertFromBuilder", "Insert" + m.name + "From", m.name + "Query", m.name + "LockedQuery", m.name + "FieldChanges", m.name + "Changes", m.name + "Hooks", m.name + "Observer", "New" + m.name + "Observer", "Register" + m.name + "Observer", m.name + "RetrievalHooks", m.name + "RetrievalObserver", "New" + m.name + "RetrievalObserver", "Register" + m.name + "RetrievalObserver", "Compare" + m.name, "foundry" + m.name + "Snapshot", m.query}
		symbols = append(symbols, m.name+"AuditPolicy", m.name+"AuditFieldSet", m.name+"AuditFields", m.name+"Auditing")
		if len(m.extensions) > 0 {
			symbols = append(symbols, m.name+"ExtensionSet", m.name+"ExtensionSlots", m.name+"Extensions", m.name+"ExtensionOwner", m.name+"ExtensionDeclaration")
			if !names[extensionsFunction] {
				symbols = append(symbols, extensionsFunction)
			}
		}
		for _, f := range m.fields {
			if f.input != nil {
				if !f.nullable {
					symbols = append(symbols, inputFieldName(m.name, f))
				}
				f.nullable = true
				symbols = append(symbols, inputFieldName(m.name, f))
			}
		}
		if m.softDelete != "" {
			symbols = append(symbols, "ForceDelete"+m.name+"Using")
		}
		for _, symbol := range symbols {
			if names[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", m.position, symbol)
			}
			names[symbol] = true
		}
	}
	for i := range result.projections {
		p := &result.projections[i]
		markEnumFields(p.fields, enums)
		for _, symbol := range projectionSymbols(p.name) {
			if names[symbol] {
				return result, fmt.Errorf("%s: duplicate generated symbol %s", p.position, symbol)
			}
			names[symbol] = true
		}
	}
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
	models := make(map[*types.Named]bool)
	for _, m := range result.models {
		models[m.typ] = true
	}
	for i := range result.unions {
		declaration := &result.unions[i]
		if err := resolveJSONGraph(p, &declaration.dtoGraph, declaration.typ, declaration.typ.Obj().Pos(), models, true); err != nil {
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
		if err := resolveMultipartFields(p, declaration, models); err != nil {
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
		if err := resolveDTOSchema(p, declaration, models); err != nil {
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
	if err := inferMetadataContracts(p, result.models, result.dtos); err != nil {
		return result, err
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
	sort.Slice(result.models, func(i, j int) bool { return result.models[i].name < result.models[j].name })
	sort.Slice(result.enums, func(i, j int) bool { return result.enums[i].name < result.enums[j].name })
	sort.Slice(result.projections, func(i, j int) bool { return result.projections[i].name < result.projections[j].name })
	return result, nil
}

func markEnumFields(fields []field, enums map[*types.Named]bool) {
	for i := range fields {
		if named, ok := types.Unalias(fields[i].base).(*types.Named); ok && (enums[named] || hasEnumDescriptor(named)) {
			fields[i].kind = "Scalar"
			fields[i].enum = true
		}
	}
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
		if kind != "model" && kind != "enum" && kind != "projection" && kind != "path" && kind != "dto" && kind != "query" && kind != "form" && kind != "multipart" && kind != "message" && kind != "config" && kind != "union" {
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

func discoverModel(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (model, error) {
	m := model{name: spec.Name.Name, typ: named, position: p.fset.Position(spec.Pos())}
	for key := range args {
		if key != "table" && key != "primary" && key != "hooks" && key != "retrieval" && key != "timestamps" && key != "soft_deletes" && key != "extension_owner" {
			return m, p.diagnostic(spec.Pos(), "unsupported model option "+key)
		}
	}
	m.hooks = args["hooks"]
	m.retrieval = args["retrieval"]
	if err := discoverHooks(p, m); err != nil {
		return m, err
	}
	m.table = args["table"]
	parts := strings.Split(m.table, ".")
	if !sqlname.Table(m.table) {
		return m, p.diagnostic(spec.Pos(), "model requires a valid table name")
	}
	m.query = "Query" + exportedName(parts[len(parts)-1])
	primary := args["primary"]
	if primary == "" {
		primary = "ID"
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return m, p.diagnostic(spec.Pos(), "model must be a struct")
	}
	columns := make(map[string]bool)
	foundPrimary := false
	for i := 0; i < structure.NumFields(); i++ {
		v := structure.Field(i)
		tag, err := fieldTag(structure.Tag(i))
		if err != nil {
			return m, p.diagnostic(v.Pos(), err.Error())
		}
		if tag["-"] != "" {
			continue
		}
		if !v.Exported() || v.Embedded() {
			return m, p.diagnostic(v.Pos(), "persisted fields must be exported, non-embedded fields; use foundry:\"-\" to skip")
		}
		if slot, ok, err := discoverExtensionSlot(p, v, tag, named); err != nil {
			return m, err
		} else if ok {
			m.extensions = append(m.extensions, slot)
			continue
		}
		if wrapper, ok := types.Unalias(v.Type()).(*types.Named); ok && isNamed(wrapper, framework+"/database/relation", "Value") {
			if len(tag) != 0 {
				return m, p.diagnostic(v.Pos(), "aggregate slots cannot declare persistence column/default tags")
			}
			m.aggregates = append(m.aggregates, aggregateField{name: v.Name(), typ: wrapper.TypeArgs().At(0)})
			continue
		}
		if wrapper, ok := types.Unalias(v.Type()).(*types.Named); ok && (isNamed(wrapper, framework+"/database/relation", "One") || isNamed(wrapper, framework+"/database/relation", "Many") || isNamed(wrapper, framework+"/database/relation", "Through")) {
			if len(tag) != 0 {
				return m, p.diagnostic(v.Pos(), "relation fields cannot declare persistence column/default tags")
			}
			for typ := range wrapper.TypeArgs().Len() {
				if named, ok := types.Unalias(wrapper.TypeArgs().At(typ)).(*types.Named); !ok || named.TypeParams().Len() != 0 {
					return m, p.diagnostic(v.Pos(), "relation target and pivot must be concrete named models")
				}
			}
			r := relationField{name: v.Name(), kind: wrapper.Obj().Name(), target: wrapper.TypeArgs().At(0)}
			if wrapper.TypeArgs().Len() == 2 {
				r.pivot = wrapper.TypeArgs().At(1)
			}
			m.relations = append(m.relations, r)
			continue
		}
		if tag["name"] != "" {
			return m, p.diagnostic(v.Pos(), "foundry:\"name=...\" applies only to extension slot fields; use column=name for a persisted field")
		}
		f, err := discoverValueField(p, v, tag)
		if err != nil {
			return m, err
		}
		f.primary = v.Name() == primary
		if columns[f.column] {
			return m, p.diagnostic(v.Pos(), "duplicate column "+f.column)
		}
		columns[f.column] = true
		if f.primary {
			if !types.Comparable(f.base) {
				return m, p.diagnostic(v.Pos(), "primary key must be a comparable value; binary fields cannot be model identity keys")
			}
			if passwordHash(f.base) {
				return m, p.diagnostic(v.Pos(), "password hashes cannot be model identity keys")
			}
			foundPrimary = true
			if f.nullable {
				return m, p.diagnostic(v.Pos(), "primary key cannot be nullable")
			}
			if id, ok := types.Unalias(f.base).(*types.Named); ok && isNamed(id, framework+"/model", "ID") && !types.Identical(id.TypeArgs().At(0), named) {
				return m, p.diagnostic(v.Pos(), "primary model.ID must belong to this model")
			}
			if args["primary"] == "" {
				id, ok := types.Unalias(f.base).(*types.Named)
				if !ok || !isNamed(id, framework+"/model", "ID") || !types.Identical(id.TypeArgs().At(0), named) {
					return m, p.diagnostic(v.Pos(), "default primary ID must use model.ID owned by this model; declare primary=Field for a natural key")
				}
			}
		}
		m.fields = append(m.fields, f)
	}
	if !foundPrimary {
		return m, p.diagnostic(spec.Pos(), "model is missing primary field "+primary)
	}
	if err := discoverFieldMethods(p, &m); err != nil {
		return m, err
	}
	if err := discoverGlobalScopeSource(p, m); err != nil {
		return m, err
	}
	if err := discoverTimestamps(p, &m, args["timestamps"]); err != nil {
		return m, err
	}
	if err := discoverSoftDeletes(p, &m, args["soft_deletes"]); err != nil {
		return m, err
	}
	if err := finishExtensions(p, spec, &m, args["extension_owner"]); err != nil {
		return m, err
	}
	return m, nil
}

func discoverValueField(p *packageInput, v *types.Var, tag map[string]string) (field, error) {
	f := field{name: v.Name(), column: tag["column"], typ: v.Type(), base: v.Type(), databaseDefault: tag["default"] == "database", position: p.fset.Position(v.Pos())}
	if f.column == "" {
		f.column = snake(v.Name())
	}
	if !sqlname.Valid(f.column) {
		return f, p.diagnostic(v.Pos(), "invalid column name")
	}
	if wrapped, ok := types.Unalias(f.base).(*types.Named); ok && isNamed(wrapped, framework+"/value", "Nullable") {
		f.nullable = true
		f.base = wrapped.TypeArgs().At(0)
	}
	kind, err := fieldKind(f.base)
	if err != nil {
		return f, p.diagnostic(v.Pos(), err.Error())
	}
	f.kind = kind
	return f, nil
}

func passwordHash(typ types.Type) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	return ok && isNamed(named, framework+"/auth/password", "Hash")
}

func fieldKind(typ types.Type) (string, error) {
	if binaryType(typ) {
		return "Binary", nil
	}
	if passwordHash(typ) {
		return "Scalar", nil
	}
	base := types.Unalias(typ)
	if named, ok := base.(*types.Named); ok {
		if isNamed(named, framework+"/value", "JSON") {
			return "JSON", nil
		}
		if isNamed(named, framework+"/model", "ID") {
			return "Scalar", nil
		}
		if isNamed(named, framework+"/decimal", "Decimal") {
			return "Exact", nil
		}
		if isNamed(named, framework+"/temporal", "Interval") {
			return "Interval", nil
		}
		if named.Obj().Pkg() != nil && ((named.Obj().Pkg().Path() == framework+"/temporal" && map[string]bool{"Date": true, "Time": true, "DateTime": true, "LocalDateTime": true}[named.Obj().Name()]) || isNamed(named, "time", "Time")) {
			return "Ordered", nil
		}
	}
	if scalar, ok := base.Underlying().(*types.Basic); ok {
		switch scalar.Kind() {
		case types.String:
			return "Text", nil
		case types.Bool:
			return "Scalar", nil
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64, types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
			return "Exact", nil
		case types.Float32, types.Float64:
			return "Float", nil
		}
	}
	return "", fmt.Errorf("unsupported persisted field type; use a supported scalar, []byte, model.ID, decimal.Decimal, temporal value, password.Hash, value.JSON, or value.Nullable of one")
}

// A named byte slice is supported; a slice of a distinct named octet is not
// assignable to ~[]byte and must be rejected before emitting codec references.
func binaryType(typ types.Type) bool {
	slice, ok := types.Unalias(typ).Underlying().(*types.Slice)
	return ok && types.Identical(types.Unalias(slice.Elem()), types.Typ[types.Uint8])
}

func isNamed(named *types.Named, path, name string) bool {
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == path && named.Obj().Name() == name
}
func (p *packageInput) diagnostic(pos token.Pos, message string) error {
	return fmt.Errorf("%s: %s", p.fset.Position(pos), message)
}

func fieldTag(tag string) (map[string]string, error) {
	result := make(map[string]string)
	tags, err := parseTags(tag)
	if err != nil {
		return nil, err
	}
	text, ok := tags["foundry"]
	if !ok {
		return result, nil
	}
	if text == "-" {
		result["-"] = "true"
		return result, nil
	}
	for _, part := range strings.Split(text, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || value == "" || (key != "column" && key != "default" && key != "name") || (key == "default" && value != "database") {
			return nil, fmt.Errorf("unsupported foundry field tag; expected column=name, default=database, name=stored_name for an extension slot, or -")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate foundry field tag option")
		}
		result[key] = value
	}
	return result, nil
}

// Reject malformed/duplicate tags instead of letting reflect.StructTag.Lookup
// silently hide a misspelled persistence declaration.
func parseTags(text string) (map[string]string, error) {
	tags := make(map[string]string)
	for text != "" {
		text = strings.TrimLeft(text, " ")
		if text == "" {
			break
		}
		colon := strings.IndexByte(text, ':')
		if colon < 1 {
			return nil, fmt.Errorf("malformed struct tag")
		}
		key := text[:colon]
		for _, r := range key {
			if r <= ' ' || r == '"' || r == 127 {
				return nil, fmt.Errorf("malformed struct tag key")
			}
		}
		remaining := text[colon+1:]
		if len(remaining) == 0 || remaining[0] != '"' {
			return nil, fmt.Errorf("struct tag values must be quoted")
		}
		quoted, err := strconv.QuotedPrefix(remaining)
		if err != nil {
			return nil, fmt.Errorf("malformed quoted struct tag")
		}
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, fmt.Errorf("invalid struct tag value")
		}
		if _, exists := tags[key]; exists {
			return nil, fmt.Errorf("duplicate struct tag namespace %s", key)
		}
		tags[key] = value
		text = remaining[len(quoted):]
		if text != "" && text[0] != ' ' {
			return nil, fmt.Errorf("struct tags must be separated by spaces")
		}
	}
	return tags, nil
}

func snake(name string) string {
	runes := []rune(name)
	var out strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))) {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}
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
