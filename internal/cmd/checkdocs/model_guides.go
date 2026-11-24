package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

// Retired directives and public names belong to retirement.CheckDocument. These
// old generated names depend on application model names rather than the ledger.
var generatedModelTeaching = regexp.MustCompile(`\b(?:models|reports)\.\w+Fields(?:At)?\(|\b\w+Draft\{|\bmodels\.Query\w+\(`)

const modelReferenceMarker = "<!-- model-declaration-reference -->"

func checkModelGuide(name string, data []byte) error {
	doc := string(data)
	if docsource.RequiresCompiledSource(name) && generatedModelTeaching.MatchString(doc) {
		return fmt.Errorf("%s: guide teaches a retired generated model/query API", name)
	}
	lines := strings.Split(doc, "\n")
	for i := 0; i < len(lines); i++ {
		reference := strings.TrimSpace(lines[i]) == modelReferenceMarker
		if reference {
			if i+2 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "<!-- compiled-source: ") {
				return fmt.Errorf("%s:%d: model-declaration-reference must immediately precede one compiled-source Go fence", name, i+1)
			}
			source := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "<!-- compiled-source: "), " -->")
			source, _, _ = strings.Cut(source, "#")
			if !strings.HasSuffix(source, "_test.go") && !strings.HasSuffix(source, "_foundry.gen.go") && !strings.HasPrefix(source, "database/orm/") {
				return fmt.Errorf("%s:%d: model-declaration-reference requires generated code, a test file or the ORM owner source", name, i+1)
			}
			i += 2
		}
		marker, language := docsource.FenceStart(lines[i])
		if reference && (marker == "" || language != "go") {
			return fmt.Errorf("%s:%d: model-declaration-reference requires a compiled-source Go fence", name, i+1)
		}
		if marker == "" {
			continue
		}
		start := i + 1
		for i++; i < len(lines) && !docsource.ClosesFence(lines[i], marker); i++ {
		}
		if language != "go" || reference {
			continue
		}
		code := strings.Join(lines[start:i], "\n")
		if handwrittenModelDeclaration(code) {
			return fmt.Errorf("%s:%d: handwritten model declaration teaching; declare the model with //foundry:model and foundry tags, then run foundry generate", name, start+1)
		}
	}
	return nil
}

func handwrittenModelDeclaration(code string) bool {
	prefix := ""
	if !strings.HasPrefix(strings.TrimSpace(code), "package ") {
		prefix = "package documentation\n"
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", prefix+code, parser.AllErrors)
	if err != nil && !strings.Contains(code, "func ") && !strings.Contains(code, "type ") {
		file, _ = parser.ParseFile(fset, "", "package documentation\nfunc example() {\n"+code+"\n}", parser.AllErrors)
	}
	if file == nil {
		return false
	}
	aliases := map[string]bool{"orm": true}
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(imported)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if imported == "github.com/weiloon1234/Foundry-Go/database/orm" {
			aliases[alias] = true
		} else {
			delete(aliases, alias)
		}
	}
	owned := func(expr ast.Expr, name string) bool {
		if pointer, ok := expr.(*ast.StarExpr); ok {
			expr = pointer.X
		}
		switch indexed := expr.(type) {
		case *ast.IndexExpr:
			expr = indexed.X
		case *ast.IndexListExpr:
			expr = indexed.X
		}
		selected, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return aliases["."] && isIdentifier(expr, name)
		}
		owner, ok := selected.X.(*ast.Ident)
		return ok && aliases[owner.Name] && selected.Sel.Name == name
	}
	bad := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			bad = bad || owned(n.Fun, "Define")
		case *ast.TypeSpec:
			bad = bad || owned(n.Type, "Table")
		case *ast.StructType:
			for _, field := range n.Fields.List {
				bad = bad || owned(field.Type, "Table")
			}
		case *ast.FuncDecl:
			bad = bad || n.Recv != nil && (n.Name.Name == "FoundryReference" || n.Name.Name == "FoundryIdentity")
		}
		return true
	})
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR {
			continue
		}
		for _, spec := range decl.Specs {
			for _, expr := range spec.(*ast.ValueSpec).Values {
				if call, ok := expr.(*ast.CallExpr); ok {
					bad = bad || owned(call.Fun, "IdentityOf")
				}
			}
		}
	}
	return bad
}

func isIdentifier(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}
