package modelconvert

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func (c *converter) fieldOptions(m *Model, f *ast.Field, fn string, call *ast.CallExpr) ([]string, error) {
	existing, err := fieldTags(f)
	if err != nil {
		return nil, err
	}
	var options []string
	if tag := existing["foundry"]; tag != "" {
		for _, part := range strings.Split(tag, ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok || (key != "column" && key != "default") || value == "" {
				return nil, reject("unrecognized existing persistence tag")
			}
			if key == "column" && value == modelmeta.Snake(f.Names[0].Name) {
				continue
			}
			options = append(options, part)
		}
	}
	family := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(fn, "Nullable"), "Input"), "Of")
	role := ""
	switch family {
	case "One", "Many", "Through", "Value":
		if len(options) > 0 || len(call.Args) != 2 {
			return nil, reject("relation has persistence options or a custom binding")
		}
	case "Key", "Table", "ID", "Password", "Bytes", "EncryptedText", "EncryptedJSON", "Slot":
	case "Column":
		if !defaultColumn(c.plan.analysis.TypesInfo.TypeOf(f.Type), strings.Contains(fn, "Input")) {
			role = "column"
		}
	case "Bool":
	case "Text":
	case "Number":
		if strings.Contains(types.TypeString(c.plan.analysis.TypesInfo.TypeOf(f.Type), nil), "decimal.Decimal") {
			role = "number"
		}
	case "Enum":
	case "JSON":
	case "Time":
		name := f.Names[0].Name
		if name == "CreatedAt" || name == "UpdatedAt" || name == "DeletedAt" {
			role = "time"
		}
	case "Interval":
	case "CreatedAt":
		role = "created-at"
	case "UpdatedAt":
		role = "updated-at"
	case "SoftDelete":
		role = "soft-delete"
	default:
		return nil, reject("unsupported ORM constructor " + fn)
	}
	if !strings.HasSuffix(fn, "Of") {
		return nil, reject("unrecognized ORM constructor")
	}
	if family == "Slot" {
		if len(options) > 0 {
			return nil, reject("slot has persistence options")
		}
		m.Slots = append(m.Slots, f.Names[0].Name)
	}
	if role != "" {
		options = append(options, "role="+role)
	}
	start := 2
	if strings.Contains(fn, "Input") {
		if len(call.Args) < 3 {
			return nil, reject("input carrier is missing its transformation")
		}
		signature, ok := c.plan.analysis.TypesInfo.TypeOf(call.Fun).(*types.Signature)
		if !ok || signature.Params().Len() < 3 {
			return nil, reject("input type cannot be resolved")
		}
		transform, ok := signature.Params().At(2).Type().(*types.Signature)
		if !ok || transform.Params().Len() != 1 {
			return nil, reject("input transformation cannot be resolved")
		}
		input := types.TypeString(transform.Params().At(0).Type(), func(p *types.Package) string {
			if p == c.plan.analysis.Types {
				return ""
			}
			for _, file := range c.plan.analysis.Syntax {
				if call.Pos() < file.Pos() || call.End() > file.End() {
					continue
				}
				for _, imp := range file.Imports {
					path, _ := strconv.Unquote(imp.Path.Value)
					if path == p.Path() {
						if imp.Name != nil {
							return imp.Name.Name
						}
						return p.Name()
					}
				}
			}
			return p.Name()
		})
		options = append(options, "input="+input)
		if id, ok := call.Args[2].(*ast.Ident); !ok || id.Name != "nil" {
			name, err := c.behavior(m, call.Args[2], "mutate")
			if err != nil {
				return nil, err
			}
			m.Behaviors[len(m.Behaviors)-1].Field = f.Names[0].Name
			options = append(options, "mutate="+name)
		} else {
			method := c.functions[m.Name+".Mutate"+f.Names[0].Name]
			if method == nil {
				return nil, reject("matching Mutate method is missing")
			}
			text := c.raw(method)
			m.Behaviors = append(m.Behaviors, Behavior{Name: m.Name + "." + method.Name.Name, Before: text, After: text})
		}
		start = 3
	}
	for _, option := range call.Args[start:] {
		outer, ok := option.(*ast.CallExpr)
		if !ok || c.ormFunction(outer.Fun) != "Codec" || len(outer.Args) != 1 {
			return nil, reject("custom column option expression")
		}
		factory, ok := outer.Args[0].(*ast.CallExpr)
		if !ok || len(factory.Args) != 0 {
			return nil, reject("codec must be a named zero-argument factory")
		}
		if family != "Column" && family != "Table" && family != "ID" && family != "Bool" && family != "Bytes" && family != "Password" {
			return nil, reject("codec selects a plain column; existing " + family + " capability cannot be preserved")
		}
		name, err := c.behavior(m, factory.Fun, "codec")
		if err != nil {
			return nil, err
		}
		m.Behaviors[len(m.Behaviors)-1].Field = f.Names[0].Name
		// A custom codec already selects the plain column family.
		for i := 0; i < len(options); i++ {
			if options[i] == "role=column" {
				options = append(options[:i], options[i+1:]...)
				break
			}
		}
		options = append(options, "codec="+name)
	}
	return options, nil
}
func (c *converter) tableOption(m *Model, e ast.Expr) ([]string, error) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, reject("custom table option expression")
	}
	var option string
	switch c.ormFunction(call.Fun) {
	case "NoTimestamps":
		if len(call.Args) != 0 {
			return nil, reject("dynamic timestamp option")
		}
		return []string{"timestamps=none"}, nil
	case "NoSoftDeletes":
		if len(call.Args) != 0 {
			return nil, reject("dynamic deletion option")
		}
		return []string{"softdeletes=none"}, nil
	case "WithHooks":
		option = "hooks"
	case "WithRetrievalHooks":
		option = "retrieval"
	case "WithGlobalScopes":
		option = "scopes"
	default:
		return nil, reject("custom table option expression")
	}
	if len(call.Args) != 1 {
		return nil, reject("behavior option needs one factory")
	}
	name, err := c.behavior(m, call.Args[0], option)
	if err != nil {
		return nil, err
	}
	return []string{option + "=" + name}, nil
}
func (c *converter) raw(n ast.Node) string {
	f := c.plan.analysis.Fset.File(n.Pos())
	return string(c.plan.sources[f.Name()][f.Offset(n.Pos()):f.Offset(n.End())])
}
func (c *converter) behavior(m *Model, e ast.Expr, option string) (string, error) {
	if literal, ok := e.(*ast.FuncLit); ok {
		if option == "mutate" || option == "codec" {
			return "", reject("inline mutator or codec has no named provenance")
		}
		base := strings.TrimSuffix(modelmeta.IdentityVariable(m.Name), "Identity") + strings.ToUpper(option[:1]) + option[1:]
		name := base
		for suffix := 2; c.plan.analysis.Types.Scope().Lookup(name) != nil || c.generatedNames[name]; suffix++ {
			name = base + strconv.Itoa(suffix)
		}
		c.generatedNames[name] = true
		before := c.raw(literal)
		after := "func " + name + strings.TrimPrefix(before, "func")
		c.insert(c.plan.analysis.Fset.File(e.Pos()).Pos(len(c.plan.sources[c.plan.analysis.Fset.File(e.Pos()).Name()])), "\n\n"+after+"\n")
		m.Behaviors = append(m.Behaviors, Behavior{Name: name, Before: before, After: after, Inline: true, Option: option})
		return name, nil
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", reject("behavior must be a local named function")
	}
	object, ok := c.function(id).(*types.Func)
	if !ok || object.Pkg() != c.plan.analysis.Types {
		return "", reject("behavior must resolve to a local function declaration")
	}
	function := c.functions[id.Name]
	if function == nil {
		return "", reject("behavior function body is unavailable")
	}
	body := c.raw(function)
	m.Behaviors = append(m.Behaviors, Behavior{Name: id.Name, Before: body, After: body, Option: option})
	return id.Name, nil
}
func (c *converter) tag(field *ast.Field, options []string) error {
	if len(options) == 0 {
		if field.Tag == nil {
			return nil
		}
		raw, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			return err
		}
		raw = removeFoundryTag(raw)
		if raw == "" {
			c.change(field.Tag, "")
		} else {
			c.change(field.Tag, "`"+raw+"`")
		}
		return nil
	}
	seen := map[string]bool{}
	for _, opt := range options {
		key, _, _ := strings.Cut(opt, "=")
		if seen[key] {
			return reject("conflicting " + key + " option")
		}
		seen[key] = true
	}
	value := strings.Join(options, ",")
	if field.Tag == nil {
		c.insert(field.Type.End(), " `foundry:"+strconv.Quote(value)+"`")
		return nil
	}
	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return err
	}
	// ParseTags has already validated the complete tag. Locate only its foundry value.
	start := strings.Index(raw, "foundry:")
	if start < 0 {
		raw += " foundry:" + strconv.Quote(value)
	} else {
		end := start + len("foundry:")
		quoted := raw[end:]
		n := 1
		for n < len(quoted) {
			if quoted[n] == '\\' {
				n += 2
				continue
			}
			if quoted[n] == '"' {
				n++
				break
			}
			n++
		}
		raw = raw[:end] + strconv.Quote(value) + quoted[n:]
	}
	c.change(field.Tag, "`"+raw+"`")
	return nil
}

func (c *converter) identity(m *Model) (bool, error) {
	var identity sourceDecl
	for name, d := range c.values {
		if len(d.value.Values) != 1 {
			continue
		}
		call, ok := d.value.Values[0].(*ast.CallExpr)
		if !ok || c.ormFunction(call.Fun) != "IdentityOf" || len(call.Args) != 2 {
			continue
		}
		table, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok {
			continue
		}
		base, ok := table.X.(*ast.Ident)
		if !ok || base.Name != m.Variable {
			continue
		}
		if m.Identity != "" {
			return false, reject("multiple identity declarations")
		}
		if len(d.value.Names) != 1 || table.Sel.Name != "Table" || nodeText(c.plan.analysis.Fset, call.Args[1]) != m.Variable+".Key()" {
			return false, reject("unrecognized identity binding")
		}
		m.Identity = name
		identity = d
	}
	reference := c.functions[m.Name+".FoundryReference"]
	ident := c.functions[m.Name+".FoundryIdentity"]
	if m.Identity == "" {
		if reference != nil || ident != nil {
			return false, reject("identity methods have no matching identity block")
		}
		return false, nil
	}
	if reference == nil || ident == nil {
		return false, reject("identity block is incomplete")
	}
	var outside []string
	for _, position := range c.plan.references[m.Identity] {
		inside := false
		for _, node := range []ast.Node{identity.gen, reference, ident} {
			start, end := c.plan.analysis.Fset.Position(node.Pos()), c.plan.analysis.Fset.Position(node.End())
			if position.Filename == start.Filename && position.Offset >= start.Offset && position.Offset < end.Offset {
				inside = true
				break
			}
		}
		if !inside && m.Identity != modelmeta.IdentityVariable(m.Variable) {
			outside = append(outside, position.String())
		}
	}
	if len(outside) > 0 {
		return false, reject("identity variable " + m.Identity + " is referenced outside its removed identity block at " + strings.Join(outside, ", ") + "; rename it and its references to " + modelmeta.IdentityVariable(m.Variable) + " before conversion")
	}
	for _, entry := range []struct {
		fn     *ast.FuncDecl
		method string
	}{{reference, "reference"}, {ident, "identity"}} {
		fn := entry.fn
		if len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Body == nil {
			return false, reject("unrecognized identity receiver")
		}
		receiver := fn.Recv.List[0].Names[0].Name
		expected := "{ return " + m.Identity + ".ReferenceOf(" + receiver + ") }"
		if entry.method == "identity" {
			expected = "{ return " + receiver + ".FoundryReference().Identity() }"
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), "identity.go", "package p;func f() "+expected, 0)
		if err != nil {
			return false, err
		}
		if compact(nodeText(c.plan.analysis.Fset, fn.Body)) != compact(nodeText(token.NewFileSet(), parsed.Decls[0].(*ast.FuncDecl).Body)) {
			return false, reject("identity method contains custom behavior")
		}
		if fn.Doc != nil {
			c.change(fn.Doc, "")
		}
		c.change(fn, "")
	}
	if err := c.remove(identity); err != nil {
		return false, err
	}
	return true, nil
}

func (c *converter) extensionExpressions(m *Model, members map[string]string) error {
	wanted := map[string]string{}
	for _, field := range m.Slots {
		wanted[members[field]] = field
	}
	found := map[string]string{}
	owners := map[string]bool{}
	var walk func(ast.Expr, string)
	walk = func(e ast.Expr, path string) {
		if literal, ok := e.(*ast.CompositeLit); ok {
			for _, elt := range literal.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok {
						walk(kv.Value, path+"."+id.Name)
					}
				}
			}
			return
		}
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return
		}
		if literal, ok := call.Fun.(*ast.FuncLit); ok && len(call.Args) == 0 && literal.Type.Params.NumFields() == 0 && len(literal.Body.List) > 0 {
			valid := true
			for _, statement := range literal.Body.List[:len(literal.Body.List)-1] {
				assignment, ok := statement.(*ast.AssignStmt)
				if !ok || assignment.Tok != token.DEFINE {
					valid = false
				}
			}
			last, ok := literal.Body.List[len(literal.Body.List)-1].(*ast.ReturnStmt)
			if valid && ok && len(last.Results) == 1 {
				walk(last.Results[0], path)
			}
			return
		}
		object := c.function(call.Fun)
		if object == nil || object.Pkg() == nil {
			return
		}
		pkg := object.Pkg().Path()
		if pkg != framework+"/attachments" && pkg != framework+"/translations" && pkg != framework+"/metadata" {
			return
		}
		for _, arg := range call.Args {
			binding, ok := arg.(*ast.CallExpr)
			if !ok || len(binding.Args) != 2 {
				continue
			}
			obj := c.function(binding.Fun)
			if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != framework+"/extensions" || obj.Name() != "BindSlot" {
				continue
			}
			slot, ok := binding.Args[1].(*ast.SelectorExpr)
			if !ok {
				continue
			}
			base, ok := slot.X.(*ast.Ident)
			if !ok || base.Name != m.Variable || wanted[slot.Sel.Name] == "" {
				continue
			}
			field := wanted[slot.Sel.Name]
			if found[field] != "" {
				found[field] = "!duplicate"
			} else {
				found[field] = path
			}
			if len(call.Args) > 0 {
				if owner, ok := call.Args[0].(*ast.Ident); ok {
					owners[owner.Name] = true
				} else {
					found[field] = "!dynamic owner"
				}
			}
		}
	}
	for name, d := range c.values {
		if object := c.plan.analysis.Types.Scope().Lookup(name); object != nil {
			if owner, ok := types.Unalias(object.Type()).(*types.Named); ok && owner.Obj().Pkg() != nil && owner.Obj().Pkg().Path() == framework+"/extensions" && owner.Obj().Name() == "Owner" && owner.TypeArgs().Len() == 2 {
				if model, ok := types.Unalias(owner.TypeArgs().At(0)).(*types.Named); ok && model.Obj().Pkg() == c.plan.analysis.Types && model.Obj().Name() == m.Name {
					owners[name] = true
				}
			}
		}
		if len(d.value.Values) == 1 && len(d.value.Names) == 1 {
			walk(d.value.Values[0], name)
		}
	}
	var expressions []string
	for _, field := range m.Slots {
		path := found[field]
		if path == "" || strings.HasPrefix(path, "!") {
			return reject("slot " + field + " has no unique static extension declaration")
		}
		expressions = append(expressions, path+".Describe()")
	}
	m.Slots = expressions
	for owner := range owners {
		m.Owners = append(m.Owners, owner)
	}
	sort.Strings(m.Owners)
	return nil
}

func pruneImports(data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "staged.go", data, parser.ParseComments)
	if err != nil {
		return nil, safeerror.Wrap("parse staged imports", err)
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	var removals []edit
	for _, decl := range f.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		var unused []*ast.ImportSpec
		for _, spec := range g.Specs {
			imp := spec.(*ast.ImportSpec)
			path, _ := strconv.Unquote(imp.Path.Value)
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name != "_" && name != "." && !used[name] {
				unused = append(unused, imp)
			}
		}
		if len(unused) == len(g.Specs) {
			removals = append(removals, edit{fset.Position(g.Pos()).Offset, fset.Position(g.End()).Offset, ""})
		} else {
			for _, imp := range unused {
				removals = append(removals, edit{fset.Position(imp.Pos()).Offset, fset.Position(imp.End()).Offset, ""})
			}
		}
	}
	for i := len(removals) - 1; i >= 0; i-- {
		e := removals[i]
		data = append(append([]byte(nil), data[:e.start]...), data[e.end:]...)
	}
	return formatSource(data)
}

func formatSource(data []byte) ([]byte, error) {
	result, err := format.Source(data)
	if err != nil {
		return nil, safeerror.Wrap("format converted source", err)
	}
	return result, nil
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }
func (c *converter) localReferenced(body *ast.BlockStmt, name string) bool {
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			count++
		}
		return true
	})
	return count > 1
}
func (c *converter) keyRole(call *ast.CallExpr, locals map[string]ast.Expr) (string, bool) {
	name := c.ormFunction(call.Fun)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Key" && len(call.Args) == 0 {
		if id, ok := sel.X.(*ast.Ident); ok {
			if table, ok := locals[id.Name].(*ast.CallExpr); ok && c.ormFunction(table.Fun) == "TableOf" {
				return "lookup", true
			}
		}
	}
	args := call.Args
	if name == "InputKeyOf" && len(args) == 3 {
		inner, ok := args[1].(*ast.CallExpr)
		if !ok {
			return "", false
		}
		role, valid := c.keyRole(inner, locals)
		return "input", valid && role == "lookup"
	}
	if name == "PrimaryOf" && len(args) == 2 {
		args = args[1:]
	}
	if len(args) != 1 {
		return "", false
	}
	inner, ok := args[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}
	role, ok := c.keyRole(inner, locals)
	if !ok {
		return "", false
	}
	switch name {
	case "PrimaryOf":
		if role == "lookup" {
			return "primary", true
		}
		if role == "number" || role == "text" {
			return "primary-" + role, true
		}
	case "NumberKey":
		if role == "lookup" {
			return "number", true
		}
		if role == "primary" {
			return "primary-number", true
		}
	case "TextKey":
		if role == "lookup" {
			return "text", true
		}
		if role == "primary" {
			return "primary-text", true
		}
	}
	return "", false
}

func defaultColumn(t types.Type, input bool) bool {
	nullable := false
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == framework+"/value" && n.Obj().Name() == "Nullable" {
		nullable = true
		t = n.TypeArgs().At(0)
	}
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsBoolean != 0 {
		return nullable || input
	}
	n, ok := types.Unalias(t).(*types.Named)
	return input && ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == framework+"/model" && n.Obj().Name() == "ID"
}
func removeFoundryTag(raw string) string {
	start := strings.Index(raw, "foundry:")
	if start < 0 {
		return raw
	}
	end := start + len("foundry:") + 1
	for end < len(raw) {
		if raw[end] == '\\' {
			end += 2
			continue
		}
		if raw[end] == '"' {
			end++
			break
		}
		end++
	}
	return strings.TrimSpace(raw[:start] + raw[end:])
}
