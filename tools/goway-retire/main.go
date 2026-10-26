// Command goway-retire migrates the mechanical Stage 8d3a call sites.
// It is idempotent, parses imports (including aliases), and never edits historical
// evidence, generated output, the retained query engine or archived negatives.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const framework = "github.com/weiloon1234/Foundry-Go/"

func main() {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path = filepath.ToSlash(path)
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != "." || path == "database/query" || path == "tools/goway-retire" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_foundry.gen.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, parser.ParseComments)
		if err != nil {
			return err
		}
		aliases := map[string]string{}
		byPath := map[string]string{}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			alias := filepath.Base(name)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			aliases[alias] = name
			byPath[name] = alias
		}
		changed := false
		use := func(pkg string) string {
			if alias := byPath[pkg]; alias != "" {
				return alias
			}
			alias := filepath.Base(pkg)
			if _, exists := aliases[alias]; exists {
				alias = "foundry" + alias
			}
			imp := &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(pkg)}}
			if alias != filepath.Base(pkg) {
				imp.Name = ast.NewIdent(alias)
			}
			file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{imp}}}, file.Decls...)
			file.Imports = append(file.Imports, imp)
			byPath[pkg] = alias
			aliases[alias] = pkg
			return alias
		}
		local := ""
		switch filepath.ToSlash(filepath.Dir(path)) {
		case "database/orm":
			if file.Name.Name == "orm" {
				local = framework + "database/orm"
			}
		case "database/orm/expr":
			if file.Name.Name == "expr" {
				local = framework + "database/orm/expr"
			}
		case "database/orm/rows":
			if file.Name.Name == "rows" {
				local = framework + "database/orm/rows"
			}
		case "model":
			if file.Name.Name == "model" {
				local = framework + "model"
			}
		}
		var calls []*ast.CallExpr
		ast.Inspect(file, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				calls = append(calls, c)
			}
			return true
		})
		for i := len(calls) - 1; i >= 0; i-- {
			c := calls[i]
			fun := c.Fun
			switch f := fun.(type) {
			case *ast.IndexExpr:
				fun = f.X
			case *ast.IndexListExpr:
				fun = f.X
			}
			var name *ast.Ident
			pkg, alias := "", ""
			switch f := fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := f.X.(*ast.Ident); ok {
					name = f.Sel
					alias = x.Name
					pkg = aliases[alias]
				}
			case *ast.Ident:
				name = f
				pkg = local
			}
			if name == nil {
				continue
			}
			qualify := func(n string) ast.Expr {
				if alias == "" {
					return ast.NewIdent(n)
				}
				return &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(n)}
			}
			if pkg == framework+"database/orm/expr" && name.Name == "Select" && len(c.Args) == 1 {
				name.Name = "Row"
				original := *c
				*c = ast.CallExpr{Fun: &ast.SelectorExpr{X: &original, Sel: ast.NewIdent("Value")}}
				changed = true
			}
			if pkg == framework+"database/orm/rows" {
				switch name.Name {
				case "Compile", "Preflight", "Each":
					if path == "database/orm/rows/read_terminal.go" && name.Name == "Each" {
						name.Name = "streamRows"
						changed = true
						continue
					}
					index := 0
					if name.Name == "Preflight" {
						index = 1
					}
					if name.Name == "Each" {
						index = 2
					}
					if len(c.Args) > index {
						name.Name += "Read"
						c.Args[index] = &ast.CallExpr{Fun: qualify("Read"), Args: []ast.Expr{c.Args[index]}}
						changed = true
					}
				case "Count":
					if len(c.Args) == 3 {
						c.Fun = &ast.SelectorExpr{X: &ast.CallExpr{Fun: qualify("Read"), Args: []ast.Expr{c.Args[2]}}, Sel: ast.NewIdent("Count")}
						c.Args = c.Args[:2]
						changed = true
					}
				}
			}
			if pkg == framework+"database/orm" && (name.Name == "Load" || name.Name == "LoadMissing") && len(c.Args) >= 3 {
				var parent bytes.Buffer
				_ = format.Node(&parent, set, c.Args[2])
				table := ""
				switch {
				case path == "attachments/slot_postgres_test.go", path == "database/decoding_paths_test.go", path == "database/orm/integration_adapters_test.go":
					table = "fields"
				case path == "database/orm/relation_aggregate_test.go":
					table = "a"
				case strings.HasPrefix(path, "database/orm/relation") && strings.HasSuffix(path, "_test.go"):
					table = "p"
				case strings.Contains(path, "consumer/articles/"):
					table = "articles.Articles"
				case strings.Contains(path, "consumer/retrievalqueries/"):
					table = "retrievalqueries.Members"
				case path == "tests/fixtures/goway/pgsmoke/relations_test.go":
					table = "n"
				case strings.Contains(path, "relations_golden_test.go"), strings.Contains(path, "noraw/positive/"):
					table = "models.Users"
				case strings.Contains(path, "golden_new_loading_test.go"), strings.Contains(path, "migration/examples_test.go"):
					table = "models.Orders"
				case strings.Contains(path, "integration_adapters_behavior_test.go"), strings.Contains(path, "footprint/extension_slots/"):
					table = "e.Articles"
				case strings.Contains(path, "relation_aggregates"), strings.Contains(path, "relations_joined"), strings.Contains(path, "relation_order_golden"):
					table = "j.Parents"
					if strings.Contains(parent.String(), "j.Pivot") {
						table = "j.Pivots"
					}
					if strings.Contains(parent.String(), "j.Target") {
						table = "j.Targets"
					}
				}
				if table == "" {
					return fmt.Errorf("unreviewed load owner in %s", path)
				}
				query, err := parser.ParseExpr(table + ".Query()")
				if err != nil {
					return err
				}
				relationQuery := &ast.CallExpr{Fun: &ast.SelectorExpr{X: query, Sel: ast.NewIdent("With")}, Args: c.Args[3:], Ellipsis: c.Ellipsis}
				c.Args = []ast.Expr{c.Args[0], c.Args[1], relationQuery, c.Args[2]}
				c.Ellipsis = token.NoPos
				name.Name += "From"
				changed = true
			}
			if pkg == framework+"model" && name.Name == "NewID" && len(c.Args) == 0 {
				name.Name = "NewIDAt"
				c.Args = []ast.Expr{&ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(use("time")), Sel: ast.NewIdent("Now")}}}
				changed = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			s, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := s.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch aliases[x.Name] {
			case framework + "database/orm/expr":
				switch s.Sel.Name {
				case "JSONKind", "JSONObject", "JSONArray", "JSONString", "JSONNumber", "JSONBoolean", "JSONNull":
					x.Name = use(framework + "database")
					changed = true
				}
			case framework + "database/orm":
				switch s.Sel.Name {
				case "MorphName", "Morphable":
					x.Name = use(framework + "model")
					changed = true
				}
			case framework + "jobs":
				if s.Sel.Name == "LegacyEnvelope" {
					s.Sel.Name = "EnvelopeV1"
					changed = true
				}
			}
			return true
		})
		changed = migrateTerminals(file, local, aliases, use) || changed
		if !changed {
			return nil
		}
		var output bytes.Buffer
		if err := format.Node(&output, set, file); err != nil {
			return err
		}
		if err := os.WriteFile(path, output.Bytes(), 0644); err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
