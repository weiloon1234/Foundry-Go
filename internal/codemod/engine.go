// Package codemod plans typed consumer migrations against the installed API.
package codemod

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type Status string

const (
	Resolved   Status = "resolved"
	Unresolved Status = "unresolved"
	Regenerate Status = "regenerate"
)

type Entry struct {
	File    string
	Line    int
	Status  Status
	Message string
}
type Change struct {
	File          string
	Before, After []byte
}
type Plan struct {
	Entries []Entry
	Changes []Change
}
type Options struct {
	Dir      string
	Patterns []string
}

// RuleSet owns a migration family. It returns a replacement or an unresolved
// explanation only for nodes it recognizes. Rules run against original type info.
type RuleSet interface {
	Rewrite(*FileContext, ast.Node) (ast.Node, string)
}
type FileContext struct {
	File            *ast.File
	Info            *types.Info
	Package         *types.Package
	Parents         map[ast.Node]ast.Node
	AddedImports    map[string]string
	httpReplay      map[ast.Node]httpReplayEdit
	httpReplayNames map[string]bool
}

func Inventory(ctx context.Context, options Options, rules ...RuleSet) (*Plan, error) {
	// Validation, HTTP and ORM are built-in migrations; caller-supplied rules extend them.
	// Explicit registration remains useful without duplicating findings.
	hasValidation, hasHTTP, hasORM := false, false, false
	for _, rule := range rules {
		switch rule.(type) {
		case Validation, *Validation:
			hasValidation = true
		case HTTP, *HTTP:
			hasHTTP = true
		case ORM, *ORM:
			hasORM = true
		}
	}
	if !hasValidation {
		rules = append(append([]RuleSet{}, rules...), Validation{})
	}
	if !hasHTTP {
		rules = append(append([]RuleSet{}, rules...), HTTP{})
	}
	if !hasORM {
		rules = append(append([]RuleSet{}, rules...), ORM{})
	}
	dir := options.Dir
	if dir == "" {
		dir = "."
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	packages, err := load(ctx, dir, options.Patterns, fset)
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	seen := map[string]Change{}
	entries := map[string]bool{}
	for _, pkg := range packages {
		for _, file := range pkg.files {
			name := fset.Position(file.Pos()).Filename
			// Compiled cgo sources and test mains in the build cache are not consumer input.
			rel, err := filepath.Rel(dir, name)
			if err != nil {
				return nil, err
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("codemod: selected source lies outside --dir")
			}
			before, err := os.ReadFile(name)
			if err != nil {
				return nil, err
			}
			after, found, err := rewriteFile(fset, file, pkg.info, pkg.pkg, rules)
			if err != nil {
				return nil, err
			}
			for _, entry := range found {
				key := fmt.Sprintf("%s:%d:%s:%s", entry.File, entry.Line, entry.Status, entry.Message)
				if !entries[key] {
					plan.Entries = append(plan.Entries, entry)
					entries[key] = true
				}
			}
			change := Change{name, before, after}
			if previous, ok := seen[name]; ok && !bytes.Equal(previous.After, after) {
				return nil, fmt.Errorf("codemod: conflicting package variants for %s", name)
			}
			seen[name] = change
		}
	}
	for _, change := range seen {
		if !bytes.Equal(change.Before, change.After) {
			plan.Changes = append(plan.Changes, change)
		}
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].File < plan.Changes[j].File })
	sort.Slice(plan.Entries, func(i, j int) bool {
		a, b := plan.Entries[i], plan.Entries[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Message < b.Message
	})
	return plan, nil
}

func rewriteFile(fset *token.FileSet, file *ast.File, info *types.Info, pkg *types.Package, rules []RuleSet) ([]byte, []Entry, error) {
	name := fset.Position(file.Pos()).Filename
	before, err := os.ReadFile(name)
	if err != nil {
		return nil, nil, err
	}
	if ast.IsGenerated(file) || strings.HasSuffix(name, "_foundry.gen.go") {
		return before, []Entry{{name, 1, Regenerate, "regenerate with go tool foundry generate"}}, nil
	}
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	fc := &FileContext{File: file, Info: info, Package: pkg, Parents: parents, AddedImports: map[string]string{}}
	replacements := map[ast.Node]ast.Node{}
	var entries []Entry
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		for _, rule := range rules {
			imports := maps.Clone(fc.AddedImports)
			replacement, message := rule.Rewrite(fc, n)
			if replacement == nil {
				fc.AddedImports = imports
			}
			if message == "" {
				continue
			}
			status := Unresolved
			if replacement != nil {
				status = Resolved
				replacements[n] = replacement
			}
			entries = append(entries, Entry{name, fset.Position(n.Pos()).Line, status, message})
			break
		}
		return true
	})
	if len(replacements) == 0 {
		return before, entries, nil
	}
	var paths []string
	for path := range fc.AddedImports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		spec := &ast.ImportSpec{Name: ast.NewIdent(fc.AddedImports[path]), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}}
		file.Imports = append(file.Imports, spec)
		file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, file.Decls...)
	}
	// Replacements retain original child nodes so nested edits compose.
	replaceNodes(reflect.ValueOf(file), replacements)
	removeUnusedImports(fc)
	var output bytes.Buffer
	if err := format.Node(&output, fset, file); err != nil {
		return nil, nil, err
	}
	return output.Bytes(), entries, nil
}

// Reordered generic constructors can infer type arguments that were the only
// use of an import. Original references use object identity; synthesized package
// selectors use the aliases selected by packageAlias.
func removeUnusedImports(fc *FileContext) {
	used := map[string]bool{}
	ast.Inspect(fc.File, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		object := fc.Info.Uses[id]
		if object == nil {
			used[id.Name] = true
		} else if _, ok := object.(*types.PkgName); ok {
			used[id.Name] = true
		}
		return true
	})
	keep := map[*ast.ImportSpec]bool{}
	imports := make([]*ast.ImportSpec, 0, len(fc.File.Imports))
	for _, spec := range fc.File.Imports {
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		} else if object, ok := fc.Info.Implicits[spec].(*types.PkgName); ok {
			name = object.Name()
		}
		if name == "_" || name == "." || name == "C" || used[name] {
			keep[spec] = true
			imports = append(imports, spec)
		}
	}
	fc.File.Imports = imports
	declarations := make([]ast.Decl, 0, len(fc.File.Decls))
	for _, declaration := range fc.File.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.IMPORT {
			declarations = append(declarations, declaration)
			continue
		}
		specs := make([]ast.Spec, 0, len(group.Specs))
		for _, spec := range group.Specs {
			if keep[spec.(*ast.ImportSpec)] {
				specs = append(specs, spec)
			}
		}
		if len(specs) != 0 {
			group.Specs = specs
			declarations = append(declarations, group)
		}
	}
	fc.File.Decls = declarations
}

// Reflection only walks AST fields, never consumer values or runtime types.
func replaceNodes(v reflect.Value, replacements map[ast.Node]ast.Node) {
	if !v.IsValid() {
		return
	}
	// Function bodies and some other AST children use concrete pointer fields,
	// rather than ast.Node interfaces. Statement-level rules replace them too.
	if v.Kind() == reflect.Pointer && !v.IsNil() && v.CanSet() {
		if node, ok := v.Interface().(ast.Node); ok {
			if replacement := replacements[node]; replacement != nil {
				value := reflect.ValueOf(replacement)
				if value.Type().AssignableTo(v.Type()) {
					v.Set(value)
				}
			}
		}
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		if n, ok := v.Interface().(ast.Node); ok {
			if replacement := replacements[n]; replacement != nil && v.CanSet() {
				v.Set(reflect.ValueOf(replacement))
			}
		}
		replaceNodes(v.Elem(), replacements)
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			replaceNodes(v.Elem(), replacements)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			replaceNodes(v.Field(i), replacements)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			replaceNodes(v.Index(i), replacements)
		}
	}
}

// Apply refuses unresolved migrations and stale inputs before writing any files.
// Each file is replaced atomically; a filesystem failure can leave earlier files
// applied. The returned count identifies that partial result.
func (p *Plan) Apply() (int, error) {
	for _, e := range p.Entries {
		if e.Status == Unresolved {
			return 0, fmt.Errorf("codemod: resolve inventory findings before applying")
		}
	}
	for _, c := range p.Changes {
		info, err := os.Lstat(c.File)
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("codemod: source is not a regular file")
		}
		current, err := os.ReadFile(c.File)
		if err != nil {
			return 0, err
		}
		if !bytes.Equal(current, c.Before) {
			return 0, fmt.Errorf("codemod: source changed since inventory: %s", c.File)
		}
	}
	for i, c := range p.Changes {
		if err := writeChange(c); err != nil {
			return i, err
		}
	}
	return len(p.Changes), nil
}
func writeChange(c Change) error {
	info, err := os.Stat(c.File)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.File), ".foundry-codemod-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(c.After)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), c.File)
}
