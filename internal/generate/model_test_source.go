package generate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func rejectTestModels(dir, display string, names []string) error {
	for _, name := range names {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), "foundry:model") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(display, name), data, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				typ, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				comments := typ.Doc
				if comments == nil {
					comments = gen.Doc
				}
				if isModelDirective(comments) {
					return modelError(&packageInput{fset: fset}, typ.Pos(), typ.Name.Name, "", "model declarations in _test.go files are not supported; move the model into a non-test Go file")
				}
			}
		}
	}
	return nil
}
func isModelDirective(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
		words := strings.Fields(text)
		if len(words) > 0 && words[0] == "foundry:model" {
			return true
		}
	}
	return false
}

func modelTypeSpecs(files []source) []*ast.TypeSpec {
	var result []*ast.TypeSpec
	for _, file := range files {
		for _, decl := range file.syntax.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				typ, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				comments := typ.Doc
				if comments == nil {
					comments = gen.Doc
				}
				if isModelDirective(comments) {
					result = append(result, typ)
				}
			}
		}
	}
	return result
}
