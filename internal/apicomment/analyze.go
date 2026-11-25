// Package apicomment checks that every exported constant, variable and
// caller-input struct field of the framework's importable packages has a doc
// comment, and stages the declarations that still lack one in a recorded file
// that may only shrink.
package apicomment

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

// Declaration kinds, as the staging file records them.
const (
	KindConst = "const"
	KindVar   = "var"
	KindField = "field"
)

const frameworkPath = "github.com/weiloon1234/Foundry-Go"

// Declaration is one declaration that needs a comment. Package is the
// root-relative directory of the package that declares it; Name is Const, Var or
// Type.Field (a field of an anonymous struct is named through its owner, such as
// Type.Field.Inner).
type Declaration struct {
	Package string
	Kind    string
	Name    string
	File    string
	Line    int
	// Problem is empty when the declaration has an acceptable comment.
	Problem string
}

// Commented reports whether the declaration has an acceptable comment.
func (d Declaration) Commented() bool { return d.Problem == "" }

// Key is the staging file's line for the declaration.
func (d Declaration) Key() string { return d.Package + "\t" + d.Kind + "\t" + d.Name }

// Analyze returns every declaration of root that needs a comment, once each,
// sorted by key. Directories named internal are loaded but their constants and
// variables are not required; their struct types count when an importable
// package exposes them as a caller input.
func Analyze(root string) ([]Declaration, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fset, configs, err := retirement.LoadSurface(root)
	if err != nil {
		return nil, err
	}
	found := make(map[string]Declaration)
	for _, config := range configs {
		for _, d := range analyzeConfig(fset, root, config) {
			if prior, ok := found[d.Key()]; !ok || worse(d, prior) {
				found[d.Key()] = d
			}
		}
	}
	result := make([]Declaration, 0, len(found))
	for _, d := range found {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result, nil
}

// worse reports whether copy a of a declaration is the one to keep over copy b
// when platform files declare it more than once: a copy without an acceptable
// comment beats a commented one; otherwise the earlier position wins.
func worse(a, b Declaration) bool {
	if a.Commented() != b.Commented() {
		return !a.Commented()
	}
	return a.File+":"+lineText(a.Line) < b.File+":"+lineText(b.Line)
}

func lineText(line int) string { return fmt.Sprintf("%08d", line) }

// site is the comment state of one declared name.
type site struct {
	multi bool
	doc   *ast.CommentGroup
	trail *ast.CommentGroup
}

type analysis struct {
	fset  *token.FileSet
	root  string
	dirs  map[string]string // import path to root-relative directory
	sites map[token.Pos]site
	seen  map[walkKey]bool
	// reached holds the exported function types and interfaces some root reached.
	reached map[string]bool
	found   map[string]Declaration
}

func analyzeConfig(fset *token.FileSet, root string, config retirement.SurfaceConfig) []Declaration {
	a := &analysis{fset: fset, root: root, dirs: map[string]string{}, sites: map[token.Pos]site{}, seen: map[walkKey]bool{}, reached: map[string]bool{}, found: map[string]Declaration{}}
	for path, pkg := range config.All {
		a.dirs[path] = pkg.Dir
		for _, file := range pkg.Files {
			a.index(file)
		}
	}
	a.walkRoots(config.Importable)
	result := make([]Declaration, 0, len(a.found))
	for _, d := range a.found {
		result = append(result, d)
	}
	return result
}

// index records the comment state of every constant, variable and field name
// declared in file.
func (a *analysis) index(file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.GenDecl:
			if node.Tok != token.CONST && node.Tok != token.VAR {
				return true
			}
			grouped := node.Lparen.IsValid()
			for _, spec := range node.Specs {
				value := spec.(*ast.ValueSpec)
				doc := value.Doc
				if doc == nil && !grouped {
					doc = node.Doc
				}
				for _, name := range value.Names {
					a.sites[name.Pos()] = site{multi: len(value.Names) > 1, doc: doc, trail: value.Comment}
				}
			}
		case *ast.StructType:
			for _, field := range node.Fields.List {
				s := site{multi: len(field.Names) > 1, doc: field.Doc, trail: field.Comment}
				if len(field.Names) == 0 {
					a.sites[embeddedIdent(field.Type).Pos()] = s
				}
				for _, name := range field.Names {
					a.sites[name.Pos()] = s
				}
			}
		}
		return true
	})
}

// embeddedIdent is the identifier that names an embedded field's type.
func embeddedIdent(expr ast.Expr) *ast.Ident {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.SelectorExpr:
			return e.Sel
		case *ast.Ident:
			return e
		default:
			return &ast.Ident{NamePos: expr.Pos()}
		}
	}
}

// require records one declaration that needs a comment and judges its comment.
func (a *analysis) require(dir, kind, name string, pos token.Pos, declared string, typ types.Type) {
	position := a.fset.Position(pos)
	file := position.Filename
	if relative, err := filepath.Rel(a.root, file); err == nil {
		file = filepath.ToSlash(relative)
	}
	d := Declaration{Package: dir, Kind: kind, Name: name, File: file, Line: position.Line}
	if s, ok := a.sites[pos]; ok {
		d.Problem = judge(s, declared, typ)
	} else {
		d.Problem = problemMissing
	}
	a.found[d.Key()] = d
}

func (a *analysis) isFramework(pkg *types.Package) bool {
	return pkg != nil && (pkg.Path() == frameworkPath || strings.HasPrefix(pkg.Path(), frameworkPath+"/"))
}
