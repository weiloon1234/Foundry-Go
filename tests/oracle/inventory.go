package oracle

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Inventory finds stable assertion sites and their owning test-file entry points.
func Inventory(dir string) (map[string]bool, []string, error) { return inventory(dir, nil) }
func SelectedSites(dir string, tests []string) (map[string]bool, error) {
	sites, _, err := inventory(dir, tests)
	return sites, err
}
func inventory(dir string, selected []string) (map[string]bool, []string, error) {
	sites := map[string]bool{}
	names := map[string]bool{}
	calls := map[string]map[string]bool{}
	functionSites := map[string][]string{}
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, data, 0)
		if err != nil {
			return nil, nil, err
		}
		var problem error
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || (name.Name != "oracleEqual" && name.Name != "oracleMatch") {
				return true
			}
			if len(call.Args) != 3 {
				problem = fmt.Errorf("invalid oracle assertion in %s", file)
				return false
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				problem = fmt.Errorf("oracle site must be a string literal in %s", file)
				return false
			}
			site, err := strconv.Unquote(literal.Value)
			if err != nil || site == "" || sites[site] {
				problem = fmt.Errorf("empty or duplicate assertion site %q in %s", site, file)
				return false
			}
			sites[site] = true
			return true
		})
		if problem != nil {
			return nil, nil, problem
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if calls[fn.Name.Name] == nil {
				calls[fn.Name.Name] = map[string]bool{}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if ok {
					calls[fn.Name.Name][id.Name] = true
					if id.Name == "oracleEqual" || id.Name == "oracleMatch" {
						site, _ := strconv.Unquote(call.Args[1].(*ast.BasicLit).Value)
						functionSites[fn.Name.Name] = append(functionSites[fn.Name.Name], site)
					}
				}
				return true
			})
		}
	}
	reaches := map[string]bool{"oracleEqual": true, "oracleMatch": true}
	for changed := true; changed; {
		changed = false
		for name, callees := range calls {
			if reaches[name] {
				continue
			}
			for callee := range callees {
				if reaches[callee] {
					reaches[name] = true
					changed = true
					break
				}
			}
		}
	}
	for name := range calls {
		if strings.HasPrefix(name, "Test") && name != "TestMain" && reaches[name] {
			names[name] = true
		}
	}
	tests := []string{}
	for name := range names {
		tests = append(tests, name)
	}
	slices.Sort(tests)
	if selected != nil {
		sites = map[string]bool{}
		seen := map[string]bool{}
		var visit func(string)
		visit = func(name string) {
			if seen[name] {
				return
			}
			seen[name] = true
			for _, site := range functionSites[name] {
				sites[site] = true
			}
			for callee := range calls[name] {
				visit(callee)
			}
		}
		for _, name := range selected {
			visit(name)
		}
	}
	return sites, tests, nil
}
