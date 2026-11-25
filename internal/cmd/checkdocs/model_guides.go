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

	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

// Retired directives and public names belong to retirement.CheckDocument. These
// old generated names depend on application model names rather than the ledger.
var generatedModelTeaching = regexp.MustCompile(`\b(?:models|reports)\.\w+Fields(?:At)?\(|\b\w+Draft\{|\bmodels\.Query\w+\(`)

const modelReferenceMarker = "<!-- model-declaration-reference -->"

func checkModelGuide(cache *markdown.Cache, name string, data []byte) error {
	parsed, err := cache.Read(name, data)
	if err != nil {
		return err
	}
	nodes := append(append([]markdown.Node(nil), parsed.Blocks...), parsed.Inlines...)
	for _, node := range nodes {
		if node.Kind != markdown.Fence && node.Kind != markdown.Indented && node.Kind != markdown.CodeSpan && node.Kind != markdown.Text {
			continue
		}
		if match := generatedModelTeaching.FindStringIndex(node.Text); match != nil {
			line := node.First + strings.Count(node.Text[:match[0]], "\n")
			if node.Kind == markdown.Fence {
				line++
			}
			return fmt.Errorf("%s:%d: guide teaches a retired generated model/query API; declare models with //foundry:model and foundry tags, then use their generated tables.", name, line)
		}
	}
	reference := false
	sourceMarker := false
	for _, block := range parsed.Blocks {
		trimmed := strings.TrimSpace(block.Text)
		if block.Kind == markdown.Comment && trimmed == modelReferenceMarker {
			reference = true
			sourceMarker = false
			continue
		}
		if reference && !sourceMarker {
			if block.Kind != markdown.Comment || !strings.HasPrefix(trimmed, "<!-- compiled-source: ") {
				return fmt.Errorf("%s:%d: model-declaration-reference must immediately precede one compiled-source Go fence; move the marker directly before its compiled-source marker.", name, block.First)
			}
			source := strings.TrimSuffix(strings.TrimPrefix(trimmed, "<!-- compiled-source: "), " -->")
			source, _, _ = strings.Cut(source, "#")
			if !strings.HasSuffix(source, "_test.go") && !strings.HasSuffix(source, "_foundry.gen.go") && !strings.HasPrefix(source, "database/orm/") {
				return fmt.Errorf("%s:%d: model-declaration-reference requires generated code, a test file or the ORM owner source; bind an eligible source file.", name, block.First)
			}
			sourceMarker = true
			continue
		}
		if reference && (block.Kind != markdown.Fence || block.Info != "go") {
			return fmt.Errorf("%s:%d: model-declaration-reference requires a compiled-source Go fence; add a Go fence directly after the compiled-source marker.", name, block.First)
		}
		if block.Kind == markdown.Fence && block.Info == "go" && !reference && handwrittenModelDeclaration(block.Text) {
			return fmt.Errorf("%s:%d: handwritten model declaration teaching; declare the model with //foundry:model and foundry tags, then run foundry generate", name, block.First+1)
		}
		reference = false
	}
	if reference {
		return fmt.Errorf("%s:0: model-declaration-reference must immediately precede one compiled-source Go fence", name)
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
