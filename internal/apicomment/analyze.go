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
			if prior, ok := found[d.Key()]; !ok || d.File+":"+lineText(d.Line) < prior.File+":"+lineText(prior.Line) {
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
	seen  map[string]bool
	found map[string]Declaration
}

func analyzeConfig(fset *token.FileSet, root string, config retirement.SurfaceConfig) []Declaration {
	a := &analysis{fset: fset, root: root, dirs: map[string]string{}, sites: map[token.Pos]site{}, seen: map[string]bool{}, found: map[string]Declaration{}}
	for path, pkg := range config.All {
		a.dirs[path] = pkg.Dir
		for _, file := range pkg.Files {
			a.index(file)
		}
	}
	for _, pkg := range config.Importable {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() {
				continue
			}
			switch obj := obj.(type) {
			case *types.Const:
				a.require(pkg.Dir, KindConst, name, obj.Pos(), name)
			case *types.Var:
				a.require(pkg.Dir, KindVar, name, obj.Pos(), name)
			case *types.Func:
				a.params(obj.Type().(*types.Signature), name)
			case *types.TypeName:
				a.exposed(obj)
			}
		}
	}
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
func (a *analysis) require(dir, kind, name string, pos token.Pos, declared string) {
	position := a.fset.Position(pos)
	file := position.Filename
	if relative, err := filepath.Rel(a.root, file); err == nil {
		file = filepath.ToSlash(relative)
	}
	d := Declaration{Package: dir, Kind: kind, Name: name, File: file, Line: position.Line}
	if s, ok := a.sites[pos]; ok {
		d.Problem = judge(s, declared)
	} else {
		d.Problem = problemMissing
	}
	a.found[d.Key()] = d
}

func (a *analysis) isFramework(pkg *types.Package) bool {
	return pkg != nil && (pkg.Path() == frameworkPath || strings.HasPrefix(pkg.Path(), frameworkPath+"/"))
}

// exposed starts the traversal from an exported type of an importable
// package: the parameters of its methods (promoted ones included), and the
// parameters of any function-typed field.
func (a *analysis) exposed(obj *types.TypeName) {
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		return
	}
	if !a.isFramework(named.Obj().Pkg()) {
		return
	}
	switch under := named.Underlying().(type) {
	case *types.Signature:
		a.params(under, obj.Name())
		return
	case *types.Interface:
		for i := 0; i < under.NumMethods(); i++ {
			if m := under.Method(i); m.Exported() {
				a.params(m.Type().(*types.Signature), obj.Name()+"."+m.Name())
			}
		}
		return
	case *types.Struct:
		for i := 0; i < under.NumFields(); i++ {
			if f := under.Field(i); f.Exported() {
				a.callbackParams(f.Type(), obj.Name()+"."+f.Name())
			}
		}
	}
	set := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < set.Len(); i++ {
		if m, ok := set.At(i).Obj().(*types.Func); ok && m.Exported() {
			a.params(m.Type().(*types.Signature), obj.Name()+"."+m.Name())
		}
	}
}

// callbackParams visits the parameters of every function type that a field
// type holds directly or through pointers, slices, arrays, maps and channels.
func (a *analysis) callbackParams(t types.Type, prefix string) {
	switch t := types.Unalias(t).(type) {
	case *types.Signature:
		a.params(t, prefix)
	case *types.Named:
		if sig, ok := t.Underlying().(*types.Signature); ok {
			a.params(sig, prefix)
		}
	case *types.Pointer:
		a.callbackParams(t.Elem(), prefix)
	case *types.Slice:
		a.callbackParams(t.Elem(), prefix)
	case *types.Array:
		a.callbackParams(t.Elem(), prefix)
	case *types.Chan:
		a.callbackParams(t.Elem(), prefix)
	case *types.Map:
		a.callbackParams(t.Key(), prefix)
		a.callbackParams(t.Elem(), prefix)
	}
}

// params visits what a caller passes to a function: its parameter types.
func (a *analysis) params(sig *types.Signature, prefix string) {
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		a.visit(p.Type(), prefix+"."+p.Name())
	}
}

// visit follows a caller-input type to the struct types it reaches.
func (a *analysis) visit(t types.Type, prefix string) {
	switch t := t.(type) {
	case *types.Alias:
		a.visit(types.Unalias(t), prefix)
	case *types.Named:
		key := types.TypeString(t, nil)
		if a.seen[key] {
			return
		}
		a.seen[key] = true
		for i := 0; i < t.TypeArgs().Len(); i++ {
			a.visit(t.TypeArgs().At(i), prefix)
		}
		if !a.isFramework(t.Obj().Pkg()) {
			return
		}
		name := t.Origin().Obj().Name()
		if st, ok := t.Underlying().(*types.Struct); ok {
			a.structure(st, name)
			return
		}
		a.visit(t.Underlying(), name)
	case *types.Struct:
		a.structure(t, prefix)
	case *types.Pointer:
		a.visit(t.Elem(), prefix)
	case *types.Slice:
		a.visit(t.Elem(), prefix)
	case *types.Array:
		a.visit(t.Elem(), prefix)
	case *types.Chan:
		a.visit(t.Elem(), prefix)
	case *types.Map:
		a.visit(t.Key(), prefix)
		a.visit(t.Elem(), prefix)
	case *types.Signature:
		a.params(t, prefix)
		for i := 0; i < t.Results().Len(); i++ {
			a.visit(t.Results().At(i).Type(), prefix+"."+t.Results().At(i).Name())
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			a.visit(m.Type(), prefix+"."+m.Name())
		}
	}
}

// structure requires a comment on every exported field of a caller-input
// struct and follows every field a caller can set or embed.
func (a *analysis) structure(st *types.Struct, owner string) {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		origin := f.Origin()
		if f.Exported() {
			if dir, ok := a.dirs[origin.Pkg().Path()]; ok {
				a.require(dir, KindField, owner+"."+f.Name(), origin.Pos(), f.Name())
			}
		}
		if f.Exported() || f.Embedded() {
			a.visit(f.Type(), owner+"."+f.Name())
		}
	}
}
