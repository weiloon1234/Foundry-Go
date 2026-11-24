// Package modelconvert stages typed model conversions and proves them before publication.
package modelconvert

import (
	"bytes"
	"context"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const framework = "github.com/weiloon1234/Foundry-Go"
const ormPath = framework + "/database/orm"

// Refusal identifies a declaration that cannot be converted without guessing.
// Refusal exists only for foundry models convert; remove it after migration.
type Refusal struct {
	Model    string
	Position token.Position
	Reason   string
}

// Model describes one recognized conversion. Options counts only written options.
// Model exists only for foundry models convert; remove it after migration.
type Model struct {
	Name, Table, Variable, TableType, Identity string
	Options                                    map[string]int
	Slots, Owners                              []string
	Behaviors                                  []Behavior
	Documentation                              []DocumentationChange
}

// Behavior retains source provenance independently of runtime function addresses.
// Behavior exists only for foundry models convert; remove it after migration.
type Behavior struct {
	Name, Before, After string
	Inline              bool
	Option              string
	Field               string
}

// File is a staged replacement relative to the analyzed package directory.
// File exists only for foundry models convert; remove it after migration.
type File struct {
	Name          string
	Before, After []byte
}

// BareTable records a declaration requiring a consumer type migration.
// BareTable exists only for foundry models convert; remove it after migration.
type BareTable struct {
	Model, Variable string
	Position        token.Position
}

// OutsideDirective identifies a declaration that remains handwritten by rule.
// OutsideDirective exists only for foundry models convert; remove it after migration.
type OutsideDirective struct {
	Model    string
	Position token.Position
	Reason   string
}

// Plan owns the source snapshot used by the proof. Analyze never writes files.
// Plan exists only for foundry models convert; remove it after migration.
type Plan struct {
	BareTables                          []BareTable
	OutsideDirective                    []OutsideDirective
	references                          map[string][]token.Position
	Directory, PackagePath, PackageName string
	Models                              []Model
	Refused                             []Refusal
	Files                               []File
	sources                             map[string][]byte
	inputs                              map[string][]byte
	analysis                            *generate.AnalysisPackage
	snapshotModels                      []Model
}

// Analyze type-loads the selected packages, resolves ORM function identities and
// returns staged sources. It neither invokes the generator nor publishes files.
// Analyze exists only for foundry models convert; remove it after migration.
func Analyze(ctx context.Context, directory string, patterns []string) ([]*Plan, error) {
	packages, err := generate.LoadAnalysis(ctx, directory, patterns)
	if err != nil {
		return nil, safeerror.Wrap("load model conversion packages", err)
	}
	references, err := identityReferences(ctx, directory, packages)
	if err != nil {
		return nil, err
	}
	var result []*Plan
	seen := map[string]bool{}
	for _, p := range packages {
		if seen[p.Types.Path()] {
			continue
		}
		seen[p.Types.Path()] = true
		plan, err := analyzePackage(p, references[p.Types.Path()])
		if err != nil {
			return nil, err
		}
		result = append(result, plan)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PackagePath < result[j].PackagePath })
	return result, nil
}

type sourceDecl struct {
	file  *ast.File
	gen   *ast.GenDecl
	spec  *ast.TypeSpec
	value *ast.ValueSpec
}
type edit struct {
	start, end int
	text       string
}
type converter struct {
	plan           *Plan
	types          map[string]sourceDecl
	values         map[string]sourceDecl
	functions      map[string]*ast.FuncDecl
	edits          map[string][]edit
	generatedNames map[string]bool
}

func analyzePackage(p *generate.AnalysisPackage, references map[string][]token.Position) (*Plan, error) {
	plan := &Plan{PackagePath: p.Types.Path(), PackageName: p.Types.Name(), sources: map[string][]byte{}, analysis: p, references: references}
	c := &converter{plan: plan, types: map[string]sourceDecl{}, values: map[string]sourceDecl{}, functions: map[string]*ast.FuncDecl{}, edits: map[string][]edit{}, generatedNames: map[string]bool{}}
	for _, f := range p.Syntax {
		name := p.Fset.Position(f.Pos()).Filename
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, safeerror.Wrap("read conversion source", err)
		}
		plan.sources[name] = data
		if plan.Directory == "" {
			plan.Directory = filepath.Dir(name)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						c.types[s.Name.Name] = sourceDecl{file: f, gen: d, spec: s}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							c.values[n.Name] = sourceDecl{file: f, gen: d, value: s}
						}
					}
				}
			case *ast.FuncDecl:
				key := d.Name.Name
				if d.Recv != nil {
					if len(d.Recv.List) == 1 {
						key = nodeText(p.Fset, d.Recv.List[0].Type) + "." + key
					}
				}
				c.functions[key] = d
			}
		}
	}
	if plan.Directory == "" {
		return plan, nil
	}
	inputs, err := packageFiles(plan.Directory)
	if err != nil {
		return nil, err
	}
	plan.inputs = map[string][]byte{}
	for name, data := range inputs {
		plan.inputs[filepath.Join(plan.Directory, name)] = data
	}
	for name, data := range plan.sources {
		if !bytes.Equal(plan.inputs[name], data) {
			return nil, reject("source changed during conversion analysis")
		}
	}
	names := make([]string, 0, len(c.values))
	for name := range c.values {
		names = append(names, name)
	}
	sort.Strings(names)
	modelVariables := map[string][]string{}
	for _, name := range names {
		if object := p.Types.Scope().Lookup(name); object != nil {
			if model := tableModel(object.Type()); model != "" {
				modelVariables[model] = append(modelVariables[model], name)
			}
		}
	}
	for _, name := range names {
		d := c.values[name]
		object := p.Types.Scope().Lookup(name)
		if object == nil {
			continue
		}
		modelName := tableModel(object.Type())
		if modelName == "" {
			continue
		}
		if reason := outsideDirective(p.Types.Path(), object.Type()); reason != "" {
			if name == modelVariables[modelName][0] {
				plan.OutsideDirective = append(plan.OutsideDirective, OutsideDirective{Model: modelName, Position: p.Fset.Position(d.value.Pos()), Reason: reason})
			}
			continue
		}
		if bareTable(object.Type()) {
			position := p.Fset.Position(d.value.Pos())
			plan.BareTables = append(plan.BareTables, BareTable{Model: modelName, Variable: name, Position: position})
			plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: position, Reason: "bare orm.Table value " + name + "; introduce " + modelName + "Table embedding " + types.TypeString(object.Type(), nil) + " and update its call sites first; the variable type changes"})
			continue
		}
		if variables := modelVariables[modelName]; len(variables) > 1 {
			if name == variables[0] {
				plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: p.Fset.Position(d.value.Pos()), Reason: "multiple table variables for one model: " + strings.Join(variables, ", ")})
			}
			continue
		}
		if model, ok := c.types[modelName]; ok && model.gen.Doc != nil {
			converted := false
			for _, comment := range model.gen.Doc.List {
				if strings.HasPrefix(comment.Text, "//foundry:model ") {
					converted = true
				}
			}
			if converted {
				model, err := c.existingSnapshotModel(name, modelName, object.Type())
				if err != nil {
					plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: p.Fset.Position(d.value.Pos()), Reason: err.Error()})
				} else {
					plan.snapshotModels = append(plan.snapshotModels, model)
				}
				continue
			}
		}
		if len(d.value.Values) != 1 {
			plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: p.Fset.Position(d.value.Pos()), Reason: "table binding is not one direct orm.Define call"})
			continue
		}
		call, ok := d.value.Values[0].(*ast.CallExpr)
		if !ok || c.ormFunction(call.Fun) != "Define" {
			plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: p.Fset.Position(d.value.Pos()), Reason: "table binding is not one direct orm.Define call"})
			continue
		}
		local := &converter{plan: plan, types: c.types, values: c.values, functions: c.functions, edits: map[string][]edit{}, generatedNames: map[string]bool{}}
		model, err := local.convert(name, d, call)
		if err != nil {
			modelName := model.Name
			if modelName == "" {
				modelName = tableModel(object.Type())
			}
			plan.Refused = append(plan.Refused, Refusal{Model: modelName, Position: p.Fset.Position(call.Pos()), Reason: err.Error()})
			continue
		}
		for file, edits := range local.edits {
			c.edits[file] = append(c.edits[file], edits...)
		}
		for n := range local.generatedNames {
			c.generatedNames[n] = true
		}
		plan.Models = append(plan.Models, model)
		plan.snapshotModels = append(plan.snapshotModels, model)
	}
	c.recordUnboundTables(modelVariables)
	for file, edits := range c.edits {
		data := append([]byte(nil), plan.sources[file]...)
		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		end := len(data)
		for _, e := range edits {
			if e.end > end {
				return nil, safeerror.New("overlapping model conversion edits")
			}
			data = append(append(append([]byte(nil), data[:e.start]...), e.text...), data[e.end:]...)
			end = e.start
		}
		data, err := format.Source(data)
		if err != nil {
			return nil, safeerror.Wrap("format staged model source", err)
		}
		// Imports used only by removed declarations belong in generated output.
		data, err = pruneImports(data)
		if err != nil {
			return nil, err
		}
		plan.Files = append(plan.Files, File{Name: filepath.Base(file), Before: append([]byte(nil), plan.sources[file]...), After: data})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Name < plan.Files[j].Name })
	return plan, nil
}
func (c *converter) ormFunction(e ast.Expr) string {
	obj := c.function(e)
	if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == ormPath {
		return obj.Name()
	}
	return ""
}
func (c *converter) function(e ast.Expr) types.Object {
	switch e := e.(type) {
	case *ast.IndexExpr:
		return c.function(e.X)
	case *ast.IndexListExpr:
		return c.function(e.X)
	case *ast.Ident:
		return c.plan.analysis.TypesInfo.Uses[e]
	case *ast.SelectorExpr:
		return c.plan.analysis.TypesInfo.Uses[e.Sel]
	}
	return nil
}
func (c *converter) change(node ast.Node, text string) {
	f := c.plan.analysis.Fset.File(node.Pos())
	c.edits[f.Name()] = append(c.edits[f.Name()], edit{f.Offset(node.Pos()), f.Offset(node.End()), text})
}
func (c *converter) insert(pos token.Pos, text string) {
	f := c.plan.analysis.Fset.File(pos)
	c.edits[f.Name()] = append(c.edits[f.Name()], edit{f.Offset(pos), f.Offset(pos), text})
}
func (c *converter) remove(d sourceDecl) error {
	if len(d.gen.Specs) != 1 {
		return safeerror.New("grouped declaration cannot be removed independently")
	}
	if d.gen.Doc != nil {
		c.change(d.gen.Doc, "")
	}
	var trailing *ast.CommentGroup
	if d.spec != nil {
		trailing = d.spec.Comment
	}
	if d.value != nil {
		trailing = d.value.Comment
	}
	if trailing != nil && trailing.Pos() >= d.gen.End() {
		c.change(trailing, "")
	}
	c.change(d.gen, "")
	return nil
}
func nodeText(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	_ = format.Node(&b, fset, n)
	return b.String()
}
func reject(reason string) error { return safeerror.New(reason) }

func (c *converter) convert(name string, d sourceDecl, call *ast.CallExpr) (Model, error) {
	m := Model{Variable: name, Options: map[string]int{}}
	if len(d.value.Names) != 1 || len(call.Args) < 2 {
		return m, reject("dynamic or multi-value table binding")
	}
	tableName := c.plan.analysis.TypesInfo.Types[call.Args[0]].Value
	if tableName == nil || tableName.Kind() != constant.String {
		return m, reject("table name is not a constant string")
	}
	m.Table = constant.StringVal(tableName)
	callback, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		return m, reject("declaration callback is not an inline binding")
	}
	if callback.Type.Params == nil || len(callback.Type.Params.List) != 2 || len(callback.Type.Params.List[0].Names) != 1 || len(callback.Type.Params.List[1].Names) != 1 || callback.Type.Results == nil || len(callback.Type.Results.List) != 1 {
		return m, reject("unrecognized declaration callback signature")
	}
	ptr, ok := c.plan.analysis.TypesInfo.TypeOf(callback.Type.Params.List[1].Type).(*types.Pointer)
	if !ok {
		return m, reject("binding model is not a concrete pointer")
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() != c.plan.analysis.Types {
		return m, reject("model is not a package-owned named struct")
	}
	m.Name = named.Obj().Name()
	md, ok := c.types[m.Name]
	if !ok {
		return m, reject("model struct is missing")
	}
	if md.spec.TypeParams != nil {
		return m, reject("generic model declarations are not supported")
	}
	st, ok := md.spec.Type.(*ast.StructType)
	if !ok {
		return m, reject("model declaration is not a struct")
	}
	if md.gen.Lparen.IsValid() {
		return m, reject("grouped model types cannot own a directive")
	}
	if md.gen.Doc != nil && strings.Contains(md.gen.Doc.Text(), "foundry:model") {
		return m, reject("model already has a directive and a handwritten binding")
	}
	rt := types.Unalias(c.plan.analysis.TypesInfo.TypeOf(callback.Type.Results.List[0].Type))
	tn, ok := rt.(*types.Named)
	if !ok || tn.Obj().Pkg() != c.plan.analysis.Types {
		return m, reject("table type is not package-owned")
	}
	m.TableType = tn.Obj().Name()
	if m.TableType != m.Name+"Table" {
		return m, reject("rename table type " + m.TableType + " to " + m.Name + "Table at " + c.plan.analysis.Fset.Position(tn.Obj().Pos()).String())
	}
	td, ok := c.types[m.TableType]
	if !ok {
		return m, reject("table type declaration is missing")
	}
	tt, ok := tn.Underlying().(*types.Struct)
	if !ok {
		return m, reject("table type is not a struct")
	}
	builder := callback.Type.Params.List[0].Names[0].Name
	receiver := callback.Type.Params.List[1].Names[0].Name
	locals := map[string]ast.Expr{}
	var literal *ast.CompositeLit
	for i, s := range callback.Body.List {
		switch s := s.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
				return m, reject("dynamic declaration assignment")
			}
			id, ok := s.Lhs[0].(*ast.Ident)
			if !ok || locals[id.Name] != nil {
				return m, reject("dynamic declaration local")
			}
			locals[id.Name] = s.Rhs[0]
		case *ast.ReturnStmt:
			if i != len(callback.Body.List)-1 || len(s.Results) != 1 {
				return m, reject("declaration must end in one table literal")
			}
			literal, _ = s.Results[0].(*ast.CompositeLit)
		default:
			return m, reject("dynamic construction in declaration callback")
		}
	}
	if literal == nil || !types.Identical(c.plan.analysis.TypesInfo.TypeOf(literal), rt) {
		return m, reject("unresolved table literal")
	}
	if len(literal.Elts) != tt.NumFields() {
		return m, reject("table literal does not bind every table member")
	}
	bindings := map[string]ast.Expr{}
	memberOrder := []string{}
	for i, e := range literal.Elts {
		member := tt.Field(i).Name()
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				return m, reject("unresolved keyed member")
			}
			member = id.Name
			e = kv.Value
		}
		if bindings[member] != nil {
			return m, reject("duplicate table member")
		}
		bindings[member] = e
		memberOrder = append(memberOrder, tt.Field(i).Name())
	}
	usedLocals := map[string]bool{}
	var resolve func(ast.Expr) (ast.Expr, error)
	resolve = func(e ast.Expr) (ast.Expr, error) {
		if id, ok := e.(*ast.Ident); ok {
			if x := locals[id.Name]; x != nil {
				if usedLocals["resolving:"+id.Name] {
					return nil, reject("cyclic declaration local")
				}
				usedLocals[id.Name] = true
				usedLocals["resolving:"+id.Name] = true
				r, err := resolve(x)
				delete(usedLocals, "resolving:"+id.Name)
				return r, err
			}
		}
		return e, nil
	}
	fields := map[string]*ast.Field{}
	fieldOrder := []string{}
	for _, f := range st.Fields.List {
		if len(f.Names) != 1 {
			return m, reject("embedded or grouped model fields are not supported")
		}
		fields[f.Names[0].Name] = f
		fieldOrder = append(fieldOrder, f.Names[0].Name)
	}
	tags := map[string][]string{}
	bound := map[string]bool{}
	key := ""
	members := map[string]string{}
	soft := ""
	keyMember := ""
	keyRole := ""
	var inputKey *ast.CallExpr
	for _, member := range memberOrder {
		e, err := resolve(bindings[member])
		if err != nil {
			return m, err
		}
		binding, ok := e.(*ast.CallExpr)
		if !ok {
			return m, reject("table member " + member + " has no recognized constructor")
		}
		fn := c.ormFunction(binding.Fun)
		if fn == "SoftDeletes" {
			if soft != "" || len(binding.Args) != 1 {
				return m, reject("invalid soft-delete capability")
			}
			arg, err := resolve(binding.Args[0])
			if err != nil {
				return m, err
			}
			b, ok := arg.(*ast.CallExpr)
			if !ok || c.ormFunction(b.Fun) != "SoftDeleteOf" {
				return m, reject("soft-delete capability does not reference a bound deletion field")
			}
			soft = member
			continue
		}
		if role, valid := c.keyRole(binding, locals); valid {
			if keyMember != "" {
				return m, reject("multiple key capability members")
			}
			keyMember = member
			keyRole = role
			if role == "input" {
				b, ok := binding.Args[0].(*ast.Ident)
				if !ok || b.Name != builder {
					return m, reject("input key uses another builder")
				}
				inputKey = binding
			}
			continue
		}
		if len(binding.Args) < 2 {
			return m, reject("unrecognized constructor for " + member)
		}
		b, ok := binding.Args[0].(*ast.Ident)
		if !ok || b.Name != builder {
			return m, reject("binding uses another builder")
		}
		unary, ok := binding.Args[1].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return m, reject("binding has no direct model field")
		}
		selector, ok := unary.X.(*ast.SelectorExpr)
		if !ok {
			return m, reject("binding has no matching field")
		}
		base, ok := selector.X.(*ast.Ident)
		if !ok || base.Name != receiver || fields[selector.Sel.Name] == nil {
			return m, reject("binding has no matching model field")
		}
		field := selector.Sel.Name
		if bound[field] {
			return m, reject("model field " + field + " is bound more than once")
		}
		bound[field] = true
		members[field] = member
		options, err := c.fieldOptions(&m, fields[field], fn, binding)
		if err != nil {
			return m, reject(field + ": " + err.Error())
		}
		tags[field] = options
		if fn == "TableOf" {
			if key != "" || member != "Table" {
				return m, reject("primary table embedding cannot be represented")
			}
			key = field
			delete(members, field)
		} else if member != field {
			if !tableMethodCollision(tt, field) {
				return m, c.memberRename(m, tt, field, member, false)
			}
			tags[field] = append(tags[field], "member="+member)
		}
	}
	for local := range locals {
		if !usedLocals[local] && !c.localReferenced(callback.Body, local) {
			return m, reject("unused declaration local " + local)
		}
	}
	if key == "" {
		return m, reject("declaration has no TableOf key")
	}
	if inputKey != nil {
		opts, err := c.fieldOptions(&m, fields[key], "InputKeyOf", inputKey)
		if err != nil {
			return m, reject(key + ": " + err.Error())
		}
		// TableOf already owns the field's persistence and codec options.
		for _, option := range opts {
			if strings.HasPrefix(option, "input=") || strings.HasPrefix(option, "mutate=") {
				tags[key] = append(tags[key], option)
			}
		}
	}
	if keyMember != "" {
		if keyMember != key && !tableMethodCollision(tt, key) {
			return m, c.memberRename(m, tt, key, keyMember, true)
		}
		members[key] = keyMember
		tags[key] = append(tags[key], "member="+keyMember)
		if keyRole != defaultKeyRole(c.plan.analysis.TypesInfo.TypeOf(fields[key].Type)) {
			tags[key] = append(tags[key], "key-role="+keyRole)
		}
	}
	if soft != "" && soft != "Soft" {
		return m, reject("rename soft-delete member " + m.TableType + "." + soft + " to " + m.TableType + ".Soft")
	}
	for _, opts := range tags {
		for _, opt := range opts {
			if opt == "role=soft-delete" && soft == "" {
				return m, reject("add Soft orm.SoftDeletable[" + m.Name + "] as the final table member before conversion; the generated capability cannot be omitted")
			}
		}
	}
	for _, field := range fieldOrder {
		if !bound[field] {
			existing, err := fieldTags(fields[field])
			if err != nil || existing["foundry"] != "-" {
				return m, reject("field " + field + " has no binding")
			}
		}
	}
	directive := []string{"table=" + m.Table, "var=" + name, "key=" + key}
	for _, option := range call.Args[2:] {
		opts, err := c.tableOption(&m, option)
		if err != nil {
			return m, err
		}
		directive = append(directive, opts...)
	}
	for _, opt := range directive {
		if opt == "timestamps=none" || opt == "softdeletes=none" {
			for field, options := range tags {
				if (opt == "timestamps=none" && (field == "CreatedAt" || field == "UpdatedAt")) || (opt == "softdeletes=none" && field == "DeletedAt") {
					for i, option := range options {
						if option == "role=time" {
							tags[field] = append(options[:i], options[i+1:]...)
							break
						}
					}
				}
			}
		}
	}
	identity, err := c.identity(&m)
	if err != nil {
		return m, err
	}
	if !identity {
		directive = append(directive, "identity=false")
	}
	for _, field := range fieldOrder {
		if !bound[field] {
			continue
		}
		if err := c.tag(fields[field], tags[field]); err != nil {
			return m, reject(field + ": " + err.Error())
		}
		for _, opt := range tags[field] {
			k, _, _ := strings.Cut(opt, "=")
			m.Options[k]++
		}
	}
	seenOptions := map[string]bool{}
	for _, opt := range directive {
		k, _, _ := strings.Cut(opt, "=")
		if seenOptions[k] {
			return m, reject("duplicate table option " + k)
		}
		seenOptions[k] = true
		m.Options[k]++
	}
	if err := c.moveMemberDocumentation(&m, td, fields, members, key, soft, tags); err != nil {
		return m, err
	}
	if err := c.extensionExpressions(&m, members); err != nil {
		return m, err
	}
	c.insert(md.gen.Pos(), "//foundry:model "+strings.Join(directive, " ")+"\n")
	if err := c.remove(td); err != nil {
		return m, err
	}
	if err := c.remove(d); err != nil {
		return m, err
	}
	return m, nil
}
func fieldTags(f *ast.Field) (map[string]string, error) {
	if f.Tag == nil {
		return map[string]string{}, nil
	}
	tag, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return nil, err
	}
	return modelmeta.ParseTags(tag)
}

func tableModel(t types.Type) string {
	if model := tableModelType(t); model != nil {
		return model.Obj().Name()
	}
	return ""
}
func tableModelType(t types.Type) *types.Named {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "Table" && n.TypeArgs().Len() == 2 {
		model, _ := types.Unalias(n.TypeArgs().At(0)).(*types.Named)
		return model
	}
	st, ok := types.Unalias(t).Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if field.Embedded() && bareTable(field.Type()) {
			return tableModelType(field.Type())
		}
	}
	return nil
}
func outsideDirective(path string, t types.Type) string {
	if path == ormPath || strings.HasPrefix(path, ormPath+"/") {
		return "outside the directive: ORM declaration-owner package tree"
	}
	if model := tableModelType(t); model != nil && model.TypeParams().Len() > 0 {
		return "outside the directive: generic model; retain its generic low-level table builder"
	}
	return ""
}

func (c *converter) existingSnapshotModel(variable, name string, t types.Type) (Model, error) {
	m := Model{Name: name, Variable: variable}
	local := &converter{plan: c.plan, types: c.types, values: c.values, functions: c.functions, edits: map[string][]edit{}}
	if _, err := local.identity(&m); err != nil {
		return m, err
	}
	st := t.Underlying().(*types.Struct)
	members := map[string]string{}
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		n, ok := types.Unalias(field.Type()).(*types.Named)
		if ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "Slot" {
			m.Slots = append(m.Slots, field.Name())
			members[field.Name()] = field.Name()
		}
	}
	if err := c.extensionExpressions(&m, members); err != nil {
		return m, err
	}
	return m, nil
}

func bareTable(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "Table"
}
func indexMember(st *types.Struct, name string) int {
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			return i
		}
	}
	return 0
}
func defaultKeyRole(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == framework+"/decimal" && n.Obj().Name() == "Decimal" {
		return "number"
	}
	if basic, ok := types.Unalias(t).Underlying().(*types.Basic); ok {
		if basic.Info()&types.IsNumeric != 0 {
			return "number"
		}
		if basic.Info()&types.IsString != 0 {
			return "text"
		}
	}
	return "lookup"
}

// recordUnboundTables includes generic and function-local declarations in the
// migration inventory, even when no package variable can be snapshotted.
func (c *converter) recordUnboundTables(variables map[string][]string) {
	seen := map[string]bool{}
	for _, file := range c.plan.analysis.Syntax {
		if strings.HasSuffix(c.plan.analysis.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			object := c.plan.analysis.TypesInfo.Defs[spec.Name]
			if object == nil {
				return true
			}
			model := tableModel(object.Type())
			if model == "" || len(variables[model]) > 0 || seen[model] {
				return true
			}
			seen[model] = true
			if reason := outsideDirective(c.plan.PackagePath, object.Type()); reason != "" {
				c.plan.OutsideDirective = append(c.plan.OutsideDirective, OutsideDirective{Model: model, Position: c.plan.analysis.Fset.Position(spec.Pos()), Reason: reason})
			} else {
				c.plan.Refused = append(c.plan.Refused, Refusal{Model: model, Position: c.plan.analysis.Fset.Position(spec.Pos()), Reason: "table has no package-level binding; move its declaration to a package-level variable before conversion"})
			}
			return true
		})
	}
}

func tableMethodCollision(table *types.Struct, name string) bool {
	for i := 0; i < table.NumFields(); i++ {
		base := table.Field(i)
		if !base.Embedded() || !bareTable(base.Type()) {
			continue
		}
		object, _, _ := types.LookupFieldOrMethod(base.Type(), true, nil, name)
		if _, ok := object.(*types.Func); ok {
			return true
		}
	}
	return false
}
func (c *converter) memberRename(m Model, table *types.Struct, name, member string, key bool) error {
	kind := "table member"
	if key {
		kind = "key member"
	}
	return reject("rename " + kind + " " + m.TableType + "." + member + " to " + m.TableType + "." + name + " at " + c.plan.analysis.Fset.Position(table.Field(indexMember(table, member)).Pos()).String())
}
