package modelcheck

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
)

// maxMigrationDefinitions bounds the history one check replays.
const maxMigrationDefinitions = 10000

type migrationKey struct{ origin, id string }

// migrationProblem is something in a migration the rule cannot read.
type migrationProblem struct {
	path, display string
	line          int
	why           string
	// words holds the words of the SQL text the migration does show, so a
	// problem is reported only where a table of a model is named. Nil reports it always.
	words map[string]bool
}

// migrationDefinition is one migrate.Definition literal read from source.
type migrationDefinition struct {
	key      migrationKey
	keyOK    bool
	keyExpr  ast.Expr
	requires ast.Expr
	sql      []*sqlSource
	problems []migrationProblem
	words    map[string]bool
	pkg      *generate.AnalysisPackage
	lit      *ast.CompositeLit
	// function is set when the literal is the whole body of a no-argument
	// function returning migrate.Definition, the shape applications register by call.
	function string
	declName string
	declPath string
	declLine int
	declCol  int
}

// migrationSet is the migration history of the loaded packages.
type migrationSet struct {
	ordered      []*migrationDefinition
	problems     []migrationProblem
	unregistered []*migrationDefinition
}

func isNamedFramework(t types.Type, pkg, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && modelmeta.IsFrameworkType(named, pkg, name)
}

func functionKey(info *types.Info, expr ast.Expr) string {
	var id *ast.Ident
	switch e := unparen(expr).(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return ""
	}
	if fn, ok := info.ObjectOf(id).(*types.Func); ok && fn.Pkg() != nil {
		return fn.Pkg().Path() + "." + fn.Name()
	}
	return ""
}

func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func sourceFiles(pkg *generate.AnalysisPackage) []*ast.File {
	var files []*ast.File
	for _, file := range pkg.Syntax {
		if !isTestFile(pkg.Fset.Position(file.Pos()).Filename) {
			files = append(files, file)
		}
	}
	return files
}

// collectMigrations reads every migrate.Definition literal of the packages,
// orders them as the registry does and finds the functions never registered.
func collectMigrations(root string, pkgs []*generate.AnalysisPackage, analyses map[*generate.AnalysisPackage]*Analysis) migrationSet {
	var set migrationSet
	var definitions []*migrationDefinition
	byFunction := make(map[string]*migrationDefinition)
	for _, pkg := range pkgs {
		info := pkg.TypesInfo
		for _, file := range sourceFiles(pkg) {
			wholeBody := make(map[*ast.CompositeLit]*ast.FuncDecl)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Body.List) != 1 || fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
					continue
				}
				result, ok := fn.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(result.Results) != 1 {
					continue
				}
				if lit, ok := unparen(result.Results[0]).(*ast.CompositeLit); ok {
					wholeBody[lit] = fn
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isNamedFramework(info.TypeOf(lit), "/database/migrate", "Definition") {
					return true
				}
				definition := readDefinition(root, pkg, lit)
				if fn := wholeBody[lit]; fn != nil {
					at := pkg.Fset.Position(fn.Name.Pos())
					definition.function = pkg.Types.Path() + "." + fn.Name.Name
					definition.declName, definition.declPath, definition.declLine, definition.declCol = fn.Name.Name, at.Filename, at.Line, at.Column
					byFunction[definition.function] = definition
				}
				definitions = append(definitions, definition)
				return true
			})
		}
	}
	if len(definitions) > maxMigrationDefinitions {
		at := pkgs[0].Fset.Position(definitions[0].lit.Pos())
		set.problems = append(set.problems, migrationProblem{path: at.Filename, display: displayPath(root, at.Filename), line: at.Line,
			why: "the module declares more than the supported number of migrations to check"})
		return set
	}

	resolving := make(map[ast.Expr]bool)
	var resolve func(pkg *generate.AnalysisPackage, expr ast.Expr, depth int) (migrationKey, bool)
	resolve = func(pkg *generate.AnalysisPackage, expr ast.Expr, depth int) (migrationKey, bool) {
		if depth > 8 || expr == nil {
			return migrationKey{}, false
		}
		info := pkg.TypesInfo
		switch e := unparen(expr).(type) {
		case *ast.CompositeLit:
			var key migrationKey
			okOrigin, okID := true, false
			for i, elt := range e.Elts {
				name, value := "", elt
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok {
						name = id.Name
					}
					value = kv.Value
				} else if i == 0 {
					name = "Origin"
				} else if i == 1 {
					name = "ID"
				}
				text, ok := constantString(info, value)
				if selector, isSelector := unparen(value).(*ast.SelectorExpr); !ok && isSelector && (selector.Sel.Name == "Origin" || selector.Sel.Name == "ID") {
					if inner, found := resolve(pkg, selector.X, depth+1); found {
						text, ok = inner.origin, true
						if selector.Sel.Name == "ID" {
							text = inner.id
						}
					}
				}
				switch name {
				case "Origin":
					key.origin, okOrigin = text, ok
				case "ID":
					key.id, okID = text, ok
				}
			}
			return key, okOrigin && okID
		case *ast.Ident:
			analysis := analyses[pkg]
			if analysis == nil || resolving[e] {
				return migrationKey{}, false
			}
			values := analysis.aliases[info.ObjectOf(e)]
			if len(values) != 1 {
				return migrationKey{}, false
			}
			resolving[e] = true
			defer delete(resolving, e)
			return resolve(pkg, values[0], depth+1)
		case *ast.SelectorExpr:
			if e.Sel.Name != "Key" {
				return migrationKey{}, false
			}
			call, ok := unparen(e.X).(*ast.CallExpr)
			if !ok {
				return migrationKey{}, false
			}
			target := byFunction[functionKey(info, call.Fun)]
			if target == nil {
				return migrationKey{}, false
			}
			return resolve(target.pkg, target.keyExpr, depth+1)
		}
		return migrationKey{}, false
	}
	byKey := make(map[migrationKey]*migrationDefinition)
	var keys []migrationKey
	for _, definition := range definitions {
		key, ok := resolve(definition.pkg, definition.keyExpr, 0)
		if !ok {
			at := definition.pkg.Fset.Position(definition.lit.Pos())
			set.problems = append(set.problems, migrationProblem{path: at.Filename, display: displayPath(root, at.Filename), line: at.Line,
				why: "its key is not made of constants, so the migration cannot be placed in the history order", words: definition.words})
			continue
		}
		if byKey[key] != nil {
			continue
		}
		definition.key, definition.keyOK = key, true
		byKey[key] = definition
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].id != keys[j].id {
			return keys[i].id < keys[j].id
		}
		return keys[i].origin < keys[j].origin
	})
	requires := func(key migrationKey) ([]migrationKey, bool) {
		definition := byKey[key]
		var result []migrationKey
		if list, ok := unparen(definition.requires).(*ast.CompositeLit); ok {
			for _, elt := range list.Elts {
				if required, ok := resolve(definition.pkg, elt, 0); ok && byKey[required] != nil {
					result = append(result, required)
				}
			}
		}
		return result, true
	}
	ordered, failure := dependency.Order(keys, requires)
	if failure != nil {
		ordered = keys
	}
	for _, key := range ordered {
		set.ordered = append(set.ordered, byKey[key])
	}

	registered := make(map[string]bool)
	for _, pkg := range pkgs {
		info := pkg.TypesInfo
		for _, file := range sourceFiles(pkg) {
			var stack []ast.Node
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, n)
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				key := functionKey(info, call.Fun)
				if byFunction[key] == nil {
					return true
				}
				for _, ancestor := range stack[:len(stack)-1] {
					if lit, ok := ancestor.(*ast.CompositeLit); ok && isNamedFramework(info.TypeOf(lit), "/database/migrate", "Definition") {
						return true
					}
				}
				registered[key] = true
				return true
			})
		}
	}
	for _, definition := range definitions {
		set.problems = append(set.problems, definition.problems...)
	}
	// A function nobody registers never runs, so its SQL is not part of the history.
	kept := set.ordered[:0]
	for _, definition := range set.ordered {
		if definition.function != "" && !registered[definition.function] {
			set.unregistered = append(set.unregistered, definition)
			continue
		}
		kept = append(kept, definition)
	}
	set.ordered = kept
	return set
}

// addWords adds the lower-cased words of text, the identifiers and keywords
// that can name a table.
func addWords(words map[string]bool, text string) {
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x80)
	}) {
		words[w] = true
	}
}

func constantString(info *types.Info, expr ast.Expr) (string, bool) {
	if tv, ok := info.Types[expr]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), true
	}
	return "", false
}

// readDefinition reads the fields of a migrate.Definition literal.
func readDefinition(root string, pkg *generate.AnalysisPackage, lit *ast.CompositeLit) *migrationDefinition {
	definition := &migrationDefinition{pkg: pkg, lit: lit, words: make(map[string]bool)}
	defer func() {
		for i := range definition.problems {
			definition.problems[i].words = definition.words
		}
	}()
	problem := func(pos token.Pos, why string) {
		at := pkg.Fset.Position(pos)
		definition.problems = append(definition.problems, migrationProblem{path: at.Filename, display: displayPath(root, at.Filename), line: at.Line, why: why})
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			problem(elt.Pos(), "the migration is declared without field names")
			continue
		}
		name, _ := kv.Key.(*ast.Ident)
		if name == nil {
			continue
		}
		switch name.Name {
		case "Key":
			definition.keyExpr = kv.Value
		case "Requires":
			definition.requires = kv.Value
		case "SQL":
			list, ok := unparen(kv.Value).(*ast.CompositeLit)
			if !ok {
				problem(kv.Value.Pos(), "SQL is not a literal list of string constants")
				continue
			}
			for _, item := range list.Elts {
				text, ok := constantString(pkg.TypesInfo, item)
				if ok {
					addWords(definition.words, text)
				} else {
					ast.Inspect(item, func(n ast.Node) bool {
						if basic, isBasic := n.(*ast.BasicLit); isBasic && basic.Kind == token.STRING {
							if fragment, err := strconv.Unquote(basic.Value); err == nil {
								addWords(definition.words, fragment)
							}
						}
						return true
					})
				}
				if !ok {
					problem(item.Pos(), "a SQL statement is built at run time, not written as a string constant")
					continue
				}
				at := pkg.Fset.Position(item.Pos())
				basic, isBasic := unparen(item).(*ast.BasicLit)
				definition.sql = append(definition.sql, &sqlSource{text: text, path: at.Filename, display: displayPath(root, at.Filename), line: at.Line,
					raw: isBasic && strings.HasPrefix(basic.Value, "`")})
			}
		}
	}
	return definition
}
