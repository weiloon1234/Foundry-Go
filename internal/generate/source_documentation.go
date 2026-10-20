package generate

import (
	"fmt"
	"go/ast"
	"go/doc/comment"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

type fieldHelp string

type sourceDocumentation struct {
	input    *packageInput
	packages map[string]*documentationPackage
	types    map[types.Type]documentedType
}

type documentationPackage struct {
	path    string
	checked *types.Package
	types   map[string]documentationType
}

type documentationFile struct {
	pkg        *documentationPackage
	imports    map[string]string
	dotImports []*types.Package
	generated  bool
}

type documentationType struct {
	file *documentationFile
	spec *ast.TypeSpec
}

type documentationExpression struct {
	file       *documentationFile
	syntax     ast.Expr
	parameters map[string]documentationExpression
}

type documentedType struct {
	parameters map[string]documentationExpression
	syntax     ast.Expr
	file       *documentationFile
	fields     map[string]*ast.Field
}

type documentationInput struct {
	dir         string
	goNames     []string
	files       map[string]fileState
	moduleFiles map[string]fileState
}

func (p *packageInput) sourceDocumentation() *sourceDocumentation {
	if p.documentation == nil {
		p.documentation = &sourceDocumentation{input: p, packages: make(map[string]*documentationPackage), types: make(map[types.Type]documentedType)}
	}
	return p.documentation
}

func (d *sourceDocumentation) packageSource(path string) (*documentationPackage, error) {
	if pkg := d.packages[path]; pkg != nil {
		return pkg, nil
	}
	pkg := &documentationPackage{path: path, types: make(map[string]documentationType)}
	var files []*ast.File
	if path == d.input.path {
		pkg.checked = d.input.types
		for _, file := range d.input.files {
			files = append(files, file.syntax)
		}
		for _, name := range sortedNames(d.input.previous) {
			syntax, _ := parser.ParseFile(token.NewFileSet(), name, d.input.previous[name], parser.ParseComments|parser.SkipObjectResolution)
			if syntax != nil {
				files = append(files, syntax)
			}
		}
	} else {
		graph, ok := d.input.importer.(*packageGraph)
		if !ok {
			return nil, fmt.Errorf("source documentation for %s has no package location; retry generation", path)
		}
		location, ok := graph.documentationPackages[path]
		if !ok || location.Dir == "" {
			return nil, fmt.Errorf("source documentation for %s is unavailable; retry generation with its source installed", path)
		}
		var err error
		pkg.checked, err = graph.Import(path)
		if err != nil {
			return nil, err
		}
		input := documentationInput{dir: location.Dir, files: make(map[string]fileState), moduleFiles: make(map[string]fileState)}
		input.goNames, err = goSourceNames(input.dir)
		if err != nil {
			return nil, fmt.Errorf("read source documentation for %s: %w", path, err)
		}
		modules, err := captureModuleFiles(input.dir)
		if err != nil {
			return nil, err
		}
		for name, file := range modules {
			input.moduleFiles[name] = state(file)
		}
		names := append(slices.Clone(location.GoFiles), location.CgoFiles...)
		for name := range graph.overlay {
			if filepath.Dir(name) == input.dir && !slices.Contains(names, filepath.Base(name)) {
				names = append(names, filepath.Base(name))
			}
		}
		slices.Sort(names)
		fset := token.NewFileSet()
		for _, name := range names {
			if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
				return nil, fmt.Errorf("invalid source documentation filename in %s", path)
			}
			before, err := readWithin(input.dir, name)
			if err != nil {
				return nil, fmt.Errorf("read source documentation for %s: %w", path, err)
			}
			input.files[name] = state(before)
			data, overlaid := graph.overlay[filepath.Join(input.dir, name)]
			if !overlaid {
				if !before.exists {
					return nil, fmt.Errorf("source documentation %s/%s disappeared during generation; retry", path, name)
				}
				data = before.data
			}
			syntax, err := parser.ParseFile(fset, filepath.Join(input.dir, name), data, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			files = append(files, syntax)
		}
		d.input.documentationInputs = append(d.input.documentationInputs, input)
	}
	for _, syntax := range files {
		file := &documentationFile{pkg: pkg, imports: make(map[string]string), generated: ast.IsGenerated(syntax)}
		for _, imported := range syntax.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return nil, err
			}
			name := ""
			if imported.Name != nil {
				name = imported.Name.Name
			} else if pkg.checked != nil {
				for _, dependency := range pkg.checked.Imports() {
					if dependency.Path() == path {
						name = dependency.Name()
						break
					}
				}
			}
			if name == "" {
				if graph, ok := d.input.importer.(*packageGraph); ok {
					name = graph.documentationPackages[path].Name
				}
			}
			if name == "." && pkg.checked != nil {
				for _, dependency := range pkg.checked.Imports() {
					if dependency.Path() == path {
						file.dotImports = append(file.dotImports, dependency)
					}
				}
			} else if name != "" && name != "_" {
				file.imports[name] = path
			}
		}
		for _, declaration := range syntax.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				spec := spec.(*ast.TypeSpec)
				pkg.types[spec.Name.Name] = documentationType{file, spec}
			}
		}
	}
	d.packages[path] = pkg
	return pkg, nil
}

func (d *sourceDocumentation) structure(typ types.Type) (documentedType, error) {
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	if source, ok := d.types[typ]; ok {
		return source, nil
	}
	var object *types.TypeName
	switch typ := typ.(type) {
	case *types.Named:
		object = typ.Obj()
	case *types.Alias:
		object = typ.Obj()
	}
	if object == nil || object.Pkg() == nil {
		return documentedType{}, fmt.Errorf("cannot locate type source documentation for %s", typ)
	}
	pkg, err := d.packageSource(object.Pkg().Path())
	if err != nil {
		return documentedType{}, err
	}
	source, err := d.declaration(pkg, object.Name(), make(map[documentationType]bool))
	if err == nil && source.file != nil {
		target := typ
		for {
			pointer, ok := types.Unalias(target).(*types.Pointer)
			if !ok {
				break
			}
			target = pointer.Elem()
		}
		d.rememberAnonymous(target, source.file, source.syntax, source.parameters)
	}
	if err == nil {
		d.types[typ] = source
	}
	return source, err
}

func (d *sourceDocumentation) jsonSource(typ types.Type) (documentedType, error) {
	for {
		if _, alias := typ.(*types.Alias); alias {
			source, err := d.structure(typ)
			if err != nil || source.file == nil {
				return source, err
			}
			inner, wrapped := jsonInner(typ)
			if !wrapped {
				return source, nil
			}
			typ = inner
			continue
		}
		inner, wrapped := jsonInner(typ)
		if !wrapped {
			return d.structure(typ)
		}
		typ = inner
	}
}

func (d *sourceDocumentation) rememberStruct(typ types.Type, source documentedType) {
	structure, ok := types.Unalias(typ).Underlying().(*types.Struct)
	if !ok || source.file == nil {
		return
	}
	d.types[typ] = source
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		if syntax := source.fields[field.Name()]; syntax != nil {
			d.rememberAnonymous(field.Type(), source.file, syntax.Type, source.parameters)
		}
	}
}

func documentationTypeArguments(typ types.Type) *types.TypeList {
	switch typ := typ.(type) {
	case *types.Named:
		return typ.TypeArgs()
	case *types.Alias:
		return typ.TypeArgs()
	}
	return nil
}

func (d *sourceDocumentation) rememberAnonymous(typ types.Type, file *documentationFile, expression ast.Expr, parameters map[string]documentationExpression) {
	switch expression := expression.(type) {
	case *ast.Ident:
		if bound, ok := parameters[expression.Name]; ok {
			d.rememberAnonymous(typ, bound.file, bound.syntax, bound.parameters)
		}
	case *ast.ParenExpr:
		d.rememberAnonymous(typ, file, expression.X, parameters)
	case *ast.StarExpr:
		if pointer, ok := types.Unalias(typ).(*types.Pointer); ok {
			d.rememberAnonymous(pointer.Elem(), file, expression.X, parameters)
		}
	case *ast.IndexExpr:
		if arguments := documentationTypeArguments(typ); arguments != nil && arguments.Len() == 1 {
			d.rememberAnonymous(arguments.At(0), file, expression.Index, parameters)
		}
	case *ast.IndexListExpr:
		if arguments := documentationTypeArguments(typ); arguments != nil && arguments.Len() == len(expression.Indices) {
			for i, argument := range expression.Indices {
				d.rememberAnonymous(arguments.At(i), file, argument, parameters)
			}
		}
	case *ast.ArrayType:
		d.types[typ] = documentedType{parameters: parameters, syntax: expression, file: file}
		switch typ := types.Unalias(typ).Underlying().(type) {
		case *types.Array:
			d.rememberAnonymous(typ.Elem(), file, expression.Elt, parameters)
		case *types.Slice:
			d.rememberAnonymous(typ.Elem(), file, expression.Elt, parameters)
		}
	case *ast.MapType:
		d.types[typ] = documentedType{parameters: parameters, syntax: expression, file: file}
		if mapping, ok := types.Unalias(typ).Underlying().(*types.Map); ok {
			d.rememberAnonymous(mapping.Elem(), file, expression.Value, parameters)
		}
	case *ast.StructType:
		source, err := d.expression(file, expression, make(map[documentationType]bool), parameters)
		if err == nil {
			d.rememberStruct(typ, source)
		}
	}
}

func (d *sourceDocumentation) declaration(pkg *documentationPackage, name string, seen map[documentationType]bool, arguments ...documentationExpression) (documentedType, error) {
	declaration, ok := pkg.types[name]
	if !ok {
		return documentedType{}, fmt.Errorf("source documentation declaration %s.%s changed or is unavailable; retry generation", pkg.path, name)
	}
	if seen[declaration] {
		return documentedType{}, fmt.Errorf("recursive source documentation declaration %s.%s", pkg.path, name)
	}
	if declaration.file.generated {
		return documentedType{}, nil
	}
	seen[declaration] = true
	defer delete(seen, declaration)
	parameters := make(map[string]documentationExpression)
	if declaration.spec.TypeParams != nil {
		index := 0
		for _, parameter := range declaration.spec.TypeParams.List {
			for _, name := range parameter.Names {
				if index < len(arguments) {
					parameters[name.Name] = arguments[index]
				}
				index++
			}
		}
	}
	return d.expression(declaration.file, declaration.spec.Type, seen, parameters)
}

func (d *sourceDocumentation) reference(file *documentationFile, expression ast.Expr) (*documentationPackage, string, error) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		if _, exists := file.pkg.types[expression.Name]; !exists {
			for _, imported := range file.dotImports {
				if _, ok := imported.Scope().Lookup(expression.Name).(*types.TypeName); ok {
					pkg, err := d.packageSource(imported.Path())
					return pkg, expression.Name, err
				}
			}
		}
		return file.pkg, expression.Name, nil
	case *ast.SelectorExpr:
		if qualifier, ok := expression.X.(*ast.Ident); ok {
			if path := file.imports[qualifier.Name]; path != "" {
				pkg, err := d.packageSource(path)
				return pkg, expression.Sel.Name, err
			}
		}
	}
	return nil, "", fmt.Errorf("cannot locate source documentation type in %s; retry generation", file.pkg.path)
}

func (d *sourceDocumentation) expression(file *documentationFile, expression ast.Expr, seen map[documentationType]bool, parameters map[string]documentationExpression) (documentedType, error) {
	var arguments []ast.Expr
	switch syntax := expression.(type) {
	case *ast.ParenExpr:
		return d.expression(file, syntax.X, seen, parameters)
	case *ast.StarExpr:
		return d.expression(file, syntax.X, seen, parameters)
	case *ast.IndexExpr:
		expression, arguments = syntax.X, []ast.Expr{syntax.Index}
	case *ast.IndexListExpr:
		expression, arguments = syntax.X, syntax.Indices
	case *ast.Ident:
		if bound, ok := parameters[syntax.Name]; ok {
			return d.expression(bound.file, bound.syntax, seen, bound.parameters)
		}
	case *ast.StructType:
		result := documentedType{parameters: parameters, syntax: syntax, file: file, fields: make(map[string]*ast.Field)}
		for _, field := range syntax.Fields.List {
			if len(field.Names) == 0 {
				result.fields[receiverTypeName(field.Type)] = field
			}
			for _, name := range field.Names {
				result.fields[name.Name] = field
			}
		}
		return result, nil
	case *ast.ArrayType, *ast.MapType:
		return documentedType{parameters: parameters, syntax: syntax, file: file}, nil
	}
	pkg, name, err := d.reference(file, expression)
	if err != nil {
		return documentedType{}, err
	}
	bound := make([]documentationExpression, len(arguments))
	for i, argument := range arguments {
		bound[i] = documentationExpression{file, argument, parameters}
	}
	return d.declaration(pkg, name, seen, bound...)
}

func (s documentedType) field(name string) (*ast.Field, error) {
	if s.file == nil {
		return nil, nil
	}
	field := s.fields[name]
	if field == nil {
		return nil, fmt.Errorf("source documentation field %s.%s changed or is unavailable; retry generation", s.file.pkg.path, name)
	}
	return field, nil
}

func (d *sourceDocumentation) child(parent documentedType, field *ast.Field, typ types.Type) (documentedType, error) {
	if parent.file == nil {
		return documentedType{}, nil
	}
	// An instantiated type parameter has no struct syntax at its use site.
	// The checked field type identifies the declaration supplying its fields.
	if name, ok := field.Type.(*ast.Ident); ok && parent.file.pkg.types[name.Name].spec == nil && parent.parameters[name.Name].syntax == nil {
		return d.structure(typ)
	}
	d.rememberAnonymous(typ, parent.file, field.Type, parent.parameters)
	source, err := d.expression(parent.file, field.Type, make(map[documentationType]bool), parent.parameters)
	if err == nil {
		d.rememberStruct(typ, source)
	}
	return source, err
}

func (d *sourceDocumentation) help(source documentedType, name string) (fieldHelp, error) {
	field, err := source.field(name)
	if err != nil || field == nil {
		return "", err
	}
	var parts []string
	for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
		if group == nil {
			continue
		}
		filtered := &ast.CommentGroup{}
		for _, item := range group.List {
			if strings.HasPrefix(item.Text, fieldNotePrefix) ||
				(strings.HasPrefix(item.Text, "//") && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(item.Text, "//")), "foundry:")) {
				continue
			}
			filtered.List = append(filtered.List, item)
		}
		if text := filtered.Text(); strings.TrimSpace(text) != "" {
			parts = append(parts, strings.Trim(text, "\n"))
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	symbolPackage := func(recv, name string) string {
		pkg := source.file.pkg
		if pkg.path == d.input.path {
			if (recv == "" && d.input.defined[name]) || (recv != "" && d.input.methods[recv][name].IsValid()) {
				return pkg.path
			}
		}
		for _, checked := range append([]*types.Package{pkg.checked}, source.file.dotImports...) {
			if checked == nil {
				continue
			}
			if recv == "" && checked.Scope().Lookup(name) != nil {
				return checked.Path()
			}
			if object := checked.Scope().Lookup(recv); object != nil {
				member, _, _ := types.LookupFieldOrMethod(object.Type(), true, checked, name)
				if member != nil {
					return checked.Path()
				}
			}
		}
		return ""
	}
	parser := comment.Parser{
		LookupPackage: func(name string) (string, bool) {
			path, ok := source.file.imports[name]
			return path, ok
		},
		LookupSym: func(recv, name string) bool { return symbolPackage(recv, name) != "" },
	}
	doc := parser.Parse(strings.Join(parts, "\n\n"))
	rewriteDocumentationLinks(doc.Content, func(link *comment.DocLink) comment.Text {
		if link.ImportPath == "" {
			link.ImportPath = symbolPackage(link.Recv, link.Name)
		}
		link.Text = []comment.Text{comment.Plain(documentationLinkName(link))}
		return link
	})
	var printer comment.Printer
	return fieldHelp(strings.TrimSuffix(string(printer.Comment(doc)), "\n")), nil
}

func documentationLinkName(link *comment.DocLink) string {
	name := link.ImportPath
	if link.Recv != "" {
		name += "." + link.Recv
	}
	if link.Name != "" {
		name += "." + link.Name
	}
	return name
}

func rewriteDocumentationLinks(blocks []comment.Block, rewrite func(*comment.DocLink) comment.Text) {
	text := func(items []comment.Text) {
		for i, item := range items {
			if link, ok := item.(*comment.DocLink); ok {
				items[i] = rewrite(link)
			}
		}
	}
	for _, block := range blocks {
		switch block := block.(type) {
		case *comment.Paragraph:
			text(block.Text)
		case *comment.Heading:
			text(block.Text)
		case *comment.List:
			for _, item := range block.Items {
				rewriteDocumentationLinks(item.Content, rewrite)
			}
		}
	}
}

func (p *packageInput) validationHelp(root types.Type, properties []jsonshape.Property[types.Type]) ([]fieldHelp, error) {
	if len(properties) == 0 {
		return nil, nil
	}
	reader := p.sourceDocumentation()
	source, err := reader.structure(root)
	if err != nil {
		return nil, err
	}
	documentation, err := reader.propertyDocumentation(root, properties, source)
	if err != nil {
		return nil, err
	}
	help := make([]fieldHelp, len(documentation))
	for i, property := range documentation {
		help[i] = property.help
	}
	return help, nil
}

type documentedProperty struct {
	help      fieldHelp
	typ       types.Type
	generated bool
}

func (reader *sourceDocumentation) propertyDocumentation(root types.Type, properties []jsonshape.Property[types.Type], source documentedType) ([]documentedProperty, error) {
	documentation := make([]documentedProperty, len(properties))
	for i, property := range properties {
		current, cursor := root, source
		for at, index := range property.Index {
			if pointer, ok := types.Unalias(current).(*types.Pointer); ok {
				current = pointer.Elem()
			}
			structure, ok := types.Unalias(current).Underlying().(*types.Struct)
			if !ok || index < 0 || index >= structure.NumFields() {
				return nil, fmt.Errorf("invalid property documentation field index")
			}
			field := structure.Field(index)
			var err error
			if at == len(property.Index)-1 {
				documentation[i].help, err = reader.help(cursor, field.Name())
				documentation[i].typ = field.Type()
				documentation[i].generated = cursor.file == nil
			} else {
				var syntax *ast.Field
				syntax, err = cursor.field(field.Name())
				if err == nil {
					cursor, err = reader.child(cursor, syntax, field.Type())
				}
			}
			if err != nil {
				return nil, err
			}
			current = field.Type()
		}
	}
	return documentation, nil
}

type emittedFieldHelp struct {
	start, end int
	help       fieldHelp
}

func fieldHelpComment(help fieldHelp) string {
	var text strings.Builder
	text.WriteString("//\n")
	for _, line := range strings.Split(string(help), "\n") {
		text.WriteString("// " + line + "\n")
	}
	text.WriteString("//\n")
	return text.String()
}

func (e *emitter) emitFieldHelp(help fieldHelp) {
	if help == "" {
		return
	}
	start := e.body.Len()
	e.body.WriteString(fieldHelpComment(help))
	e.fieldHelp = append(e.fieldHelp, emittedFieldHelp{start, e.body.Len(), help})
}

// Single-element standard import paths can be shadowed by generated aliases.
// Resolve that conflict after imports are final, within copied help only.
func (e *emitter) finishFieldHelp() []byte {
	aliases := make(map[string]string, len(e.imports))
	for path, alias := range e.imports {
		aliases[alias] = path
	}
	var edits []sourceEdit
	for _, help := range e.fieldHelp {
		var parser comment.Parser
		doc := parser.Parse(string(help.help))
		changed := false
		rewriteDocumentationLinks(doc.Content, func(link *comment.DocLink) comment.Text {
			if target := aliases[link.ImportPath]; target != "" && target != link.ImportPath && !strings.Contains(link.ImportPath, "/") {
				changed = true
				return comment.Plain(documentationLinkName(link))
			}
			return link
		})
		if changed {
			var printer comment.Printer
			text := fieldHelp(strings.TrimSuffix(string(printer.Comment(doc)), "\n"))
			edits = append(edits, sourceEdit{help.start, help.end, fieldHelpComment(text)})
		}
	}
	if len(edits) == 0 {
		return e.body.Bytes()
	}
	return applySourceEdits(e.body.Bytes(), edits)
}

func checkDocumentationInput(input documentationInput) error {
	names, err := goSourceNames(input.dir)
	if err != nil {
		return err
	}
	if !slices.Equal(names, input.goNames) {
		return fmt.Errorf("source documentation Go file set in %s changed during generation; retry", input.dir)
	}
	for _, name := range sortedNames(input.files) {
		current, err := readWithin(input.dir, name)
		if err != nil {
			return err
		}
		if state(current) != input.files[name] {
			return fmt.Errorf("source documentation %s changed during generation; retry", filepath.Join(input.dir, name))
		}
	}
	for _, name := range sortedNames(input.moduleFiles) {
		current, err := readFile(name)
		if err != nil {
			return err
		}
		if state(current) != input.moduleFiles[name] {
			return fmt.Errorf("source documentation module/workspace file %s changed during generation; retry", name)
		}
	}
	return nil
}
