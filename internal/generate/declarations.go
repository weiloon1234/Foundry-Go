package generate

import (
	"go/ast"
	"go/token"
	"strconv"
)

// A declaration unit preserves a whole const group (and therefore iota), while
// individual type/value declarations can be selected by dependency reachability.
type declarationUnit struct {
	file        int
	decl        ast.Decl
	names, refs []string
	annotation  string
	alias       bool
}

// declarationFiles type-checks model, enum, projection, path, query and DTO declarations and the symbols
// needed to understand them. Everything else is checked in the complete overlay;
// unrelated DTO aliases, functions and constants may reference generated types.
func (p *packageInput) declarationFiles() ([]*ast.File, error) {
	var units []declarationUnit
	var unionStubs []ast.Decl
	index := make(map[string]int)
	enumNames := make(map[string]bool)
	declarationMethods := make(map[string][]int)
	modelFieldMethods := make(map[string][]int)
	p.defined = make(map[string]bool)
	p.definedAt = make(map[string]token.Pos)
	p.methods = make(map[string]map[string]token.Pos)
	add := func(unit declarationUnit) {
		for _, name := range unit.names {
			index[name] = len(units)
			p.defined[name] = true
		}
		units = append(units, unit)
	}
	for fileIndex, file := range p.files {
		for _, decl := range file.syntax.Decls {
			p.recordHandwritten(decl)
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					copy := *decl
					copy.Body = nil
					add(declarationUnit{file: fileIndex, decl: &copy, names: []string{decl.Name.Name}, refs: references(decl.Type)})
				} else if (declarationMethod(decl.Name.Name) || fieldMethodPrefix(decl.Name.Name) != "" || decl.Name.Name == globalScopeSourceMethod) && len(decl.Recv.List) == 1 {
					// Codec, contract, field-method and global-scope-source discovery
					// need signatures on a fresh checkout. Bodies may reference
					// generated declarations.
					owner := receiverTypeName(decl.Recv.List[0].Type)
					copy := *decl
					copy.Body = nil
					if declarationMethod(decl.Name.Name) {
						declarationMethods[owner] = append(declarationMethods[owner], len(units))
					} else {
						modelFieldMethods[owner] = append(modelFieldMethods[owner], len(units))
					}
					add(declarationUnit{file: fileIndex, decl: &copy, refs: references(decl.Type)})
				}
			case *ast.GenDecl:
				if decl.Tok == token.IMPORT {
					continue
				}
				if decl.Tok == token.CONST {
					unit := declarationUnit{file: fileIndex, decl: decl}
					for _, spec := range decl.Specs {
						value := spec.(*ast.ValueSpec)
						for _, name := range value.Names {
							unit.names = append(unit.names, name.Name)
						}
						unit.refs = append(unit.refs, valueReferences(value)...)
					}
					add(unit)
					continue
				}
				for _, spec := range decl.Specs {
					copy := *decl
					copy.Specs = []ast.Spec{spec}
					unit := declarationUnit{file: fileIndex, decl: &copy}
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						comments := spec.Doc
						if comments == nil {
							comments = decl.Doc
						}
						kind, args, err := directive(comments)
						if err != nil {
							return nil, p.diagnostic(spec.Pos(), err.Error())
						}
						if kind == "union" {
							name := args["name"]
							if !token.IsIdentifier(name) || !token.IsExported(name) {
								return nil, p.diagnostic(spec.Pos(), "union name must be an exported Go identifier")
							}
							unionStubs = append(unionStubs, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Type: &ast.StructType{Fields: &ast.FieldList{}}}}})
						}
						unit.annotation = kind
						unit.names = []string{spec.Name.Name}
						unit.alias = spec.Assign.IsValid()
						if kind == "enum" {
							enumNames[spec.Name.Name] = true
						}
						typeCopy := *spec
						if kind == "model" {
							typ, err := p.persistedDeclaration(spec.Type)
							if err != nil {
								return nil, err
							}
							typeCopy.Type = typ
						}
						copy.Specs = []ast.Spec{&typeCopy}
						unit.refs = references(typeCopy.Type)
						if spec.TypeParams != nil {
							unit.refs = append(unit.refs, references(spec.TypeParams)...)
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							unit.names = append(unit.names, name.Name)
						}
						unit.refs = valueReferences(spec)
					}
					add(unit)
				}
			}
		}
	}
	selected := make(map[int]bool)
	for i, unit := range units {
		if unit.annotation != "" {
			selected[i] = true
		}
	}
	// Include all constants that can inherit an enum type, including aliases and
	// inferred constants in other groups. discoverEnum confirms actual Go types.
	for changed := true; changed; {
		changed = false
		for i, unit := range units {
			gen, ok := unit.decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && !unit.alias) {
				continue
			}
			if !intersects(unit.refs, enumNames) {
				continue
			}
			selected[i] = true
			for _, name := range unit.names {
				if !enumNames[name] {
					enumNames[name] = true
					changed = true
				}
			}
		}
	}
	var queue []int
	for i := range units {
		if selected[i] {
			queue = append(queue, i)
		}
	}
	for next := 0; next < len(queue); next++ {
		for _, name := range units[queue[next]].names {
			for _, i := range declarationMethods[name] {
				if !selected[i] {
					selected[i] = true
					queue = append(queue, i)
				}
			}
			// Field-method prefixes are reserved only on declared models. A
			// scalar/helper's unrelated method can still mention a generated
			// draft and belongs in the complete overlay, not this first pass.
			if units[queue[next]].annotation == "model" {
				for _, i := range modelFieldMethods[name] {
					if !selected[i] {
						selected[i] = true
						queue = append(queue, i)
					}
				}
			}
		}
		for _, name := range units[queue[next]].refs {
			if i, ok := index[name]; ok && !selected[i] {
				selected[i] = true
				queue = append(queue, i)
			}
		}
	}
	files := make([]*ast.File, len(p.files))
	for i, source := range p.files {
		copy := *source.syntax
		copy.Decls = nil
		for _, decl := range source.syntax.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				copy.Decls = append(copy.Decls, decl)
			}
		}
		files[i] = &copy
	}
	for i, unit := range units {
		if selected[i] {
			files[unit.file].Decls = append(files[unit.file].Decls, unit.decl)
		}
	}
	if len(unionStubs) != 0 {
		files = append(files, &ast.File{Name: ast.NewIdent(p.name), Decls: unionStubs})
	}
	return files, nil
}

// recordHandwritten locates package-level names and methods, including those
// omitted from declaration analysis, for generated-symbol collision reports.
func (p *packageInput) recordHandwritten(decl ast.Decl) {
	record := func(name *ast.Ident) {
		if name.Name == "_" || name.Name == "init" {
			return
		}
		if _, exists := p.definedAt[name.Name]; !exists {
			p.definedAt[name.Name] = name.Pos()
		}
	}
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if decl.Recv == nil {
			record(decl.Name)
			return
		}
		if len(decl.Recv.List) != 1 {
			return
		}
		owner := receiverTypeName(decl.Recv.List[0].Type)
		if p.methods[owner] == nil {
			p.methods[owner] = make(map[string]token.Pos)
		}
		p.methods[owner][decl.Name.Name] = decl.Name.Pos()
	case *ast.GenDecl:
		for _, spec := range decl.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				record(spec.Name)
			case *ast.ValueSpec:
				for _, name := range spec.Names {
					record(name)
				}
			}
		}
	}
}

// declarationMethod identifies signatures needed before generated output exists.
// Keep factory signatures for analysis while their bodies remain in the full
// overlay; a contract body may legitimately call another generated descriptor.
func declarationMethod(name string) bool {
	return jsonCodecMethod(name) || name == "FoundryIdentity" || name == "JSONContract" || name == "JSONKeyContract"
}

func receiverTypeName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return receiverTypeName(expr.X)
	case *ast.ParenExpr:
		return receiverTypeName(expr.X)
	case *ast.IndexExpr:
		return receiverTypeName(expr.X)
	case *ast.IndexListExpr:
		return receiverTypeName(expr.X)
	}
	return ""
}

func (p *packageInput) persistedDeclaration(expr ast.Expr) (ast.Expr, error) {
	structure, ok := expr.(*ast.StructType)
	if !ok {
		return expr, nil
	}
	copy := *structure
	fields := *structure.Fields
	fields.List = nil
	copy.Fields = &fields
	for _, field := range structure.Fields.List {
		if field.Tag != nil {
			text, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				return nil, p.diagnostic(field.Pos(), "invalid struct tag literal")
			}
			tag, err := fieldTag(text)
			if err != nil {
				return nil, p.diagnostic(field.Pos(), err.Error())
			}
			if tag["-"] != "" {
				continue
			}
		}
		fields.List = append(fields.List, field)
	}
	return &copy, nil
}

func valueReferences(spec *ast.ValueSpec) []string {
	refs := references(spec.Type)
	for _, value := range spec.Values {
		refs = append(refs, references(value)...)
	}
	return refs
}
func references(node ast.Node) []string {
	if node == nil {
		return nil
	}
	var result []string
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			result = append(result, node.Name)
		case *ast.SelectorExpr:
			result = append(result, references(node.X)...)
			return false
		case *ast.Field:
			result = append(result, references(node.Type)...)
			return false
		}
		return true
	})
	return result
}
func intersects(names []string, set map[string]bool) bool {
	for _, name := range names {
		if set[name] {
			return true
		}
	}
	return false
}
