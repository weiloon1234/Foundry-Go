package foundry_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// consumerAPIDeclaration records source declarations independently of go doc's
// alias rendering and the host's build tags. Tests and nested modules are not
// shipped APIs; internal packages cannot be imported by applications.
type consumerAPIDeclaration struct {
	Package    string
	File       string
	Line       int
	Name       string
	Kind       string
	Receiver   string `json:",omitempty"`
	Documented bool
	Example    bool
	Generated  bool
	Alias      bool `json:",omitempty"`
}

func consumerAPIDeclarations(t *testing.T) []consumerAPIDeclaration {
	t.Helper()
	var declarations []consumerAPIDeclaration
	positions := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != "." && (strings.HasPrefix(name, ".") || name == "internal" || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			if path != "." {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(positions, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if file.Name.Name == "main" {
			return nil
		}
		add := func(name, kind, receiver string, pos token.Pos, doc *ast.CommentGroup) {
			comment := doc.Text()
			declarations = append(declarations, consumerAPIDeclaration{Package: filepath.ToSlash(filepath.Dir(path)), File: filepath.ToSlash(path), Line: positions.Position(pos).Line, Name: name, Kind: kind, Receiver: receiver, Documented: strings.TrimSpace(comment) != "", Example: consumerAPIExample(comment), Generated: ast.IsGenerated(file)})
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				receiver, kind := "", "func"
				if decl.Recv != nil {
					receiver, kind = consumerAPIReceiver(decl.Recv.List[0].Type), "method"
				}
				add(decl.Name.Name, kind, receiver, decl.Pos(), decl.Doc)
			case *ast.GenDecl:
				if decl.Tok != token.TYPE {
					continue
				}
				for _, spec := range decl.Specs {
					typ := spec.(*ast.TypeSpec)
					if !typ.Name.IsExported() {
						continue
					}
					doc := typ.Doc
					if doc == nil {
						doc = decl.Doc
					}
					add(typ.Name.Name, "type", "", typ.Pos(), doc)
					declarations[len(declarations)-1].Alias = typ.Assign.IsValid()
					if iface, ok := typ.Type.(*ast.InterfaceType); ok {
						for _, field := range iface.Methods.List {
							for _, name := range field.Names {
								if name.IsExported() {
									add(name.Name, "interface method", typ.Name.Name, name.Pos(), field.Doc)
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(declarations, func(i, j int) bool {
		a, b := declarations[i], declarations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return declarations
}

func consumerAPIReceiver(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return consumerAPIReceiver(e.X)
	case *ast.IndexExpr:
		return consumerAPIReceiver(e.X)
	case *ast.IndexListExpr:
		return consumerAPIReceiver(e.X)
	case *ast.ParenExpr:
		return consumerAPIReceiver(e.X)
	default:
		panic(fmt.Sprintf("unexpected receiver %T", e))
	}
}

// TestConsumerAPIInventory provides the same source inventory used by both
// guards, including generated declarations and platform-specific files.
func TestConsumerAPIInventory(t *testing.T) {
	path := os.Getenv("FOUNDRY_PUBLIC_API_INVENTORY")
	if path == "" {
		t.Skip("set FOUNDRY_PUBLIC_API_INVENTORY to write the inventory")
	}
	data, err := json.MarshalIndent(consumerAPIDeclarations(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func consumerAPIExample(comment string) bool {
	for _, line := range strings.Split(comment, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Example:") {
			return true
		}
	}
	return false
}

func TestConsumerAPIMethodVisibility(t *testing.T) {
	allowed := make(map[string]bool)
	for receiver, methods := range consumerAPIPrivateMethodExceptions {
		for _, method := range strings.Fields(methods) {
			allowed[receiver+"."+method] = false
		}
	}
	declarations := consumerAPIDeclarations(t)
	aliases := make(map[string]bool)
	for _, declaration := range declarations {
		if declaration.Kind == "type" && declaration.Alias {
			aliases[declaration.Package+"."+declaration.Name] = true
		}
	}
	for _, declaration := range declarations {
		if declaration.Kind != "method" || ast.IsExported(declaration.Receiver) && !aliases[declaration.Package+"."+declaration.Receiver] {
			continue
		}
		key := declaration.Package + "." + declaration.Receiver + "." + declaration.Name
		if _, ok := allowed[key]; ok {
			allowed[key] = true
			continue
		}
		t.Errorf("%s:%d: %s is an exported method on a private or alias receiver; use a typed package function or an exported non-alias receiver", declaration.File, declaration.Line, key)
	}
	for key, found := range allowed {
		if !found {
			t.Errorf("remove stale private-method exception %s", key)
		}
	}
}

func TestConsumerAPIDocumentation(t *testing.T) {
	data, err := os.ReadFile("testdata/public-api-undocumented.txt")
	if err != nil {
		t.Fatal(err)
	}
	backlog := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, duplicate := backlog[line]; duplicate {
			t.Errorf("duplicate documentation backlog entry %s", line)
		}
		backlog[line] = false
	}
	for _, declaration := range consumerAPIDeclarations(t) {
		name := declaration.Name
		if declaration.Receiver != "" {
			name = declaration.Receiver + "." + name
		}
		key := declaration.File + "\t" + declaration.Kind + "\t" + name
		dsl := declaration.Package == "database/orm/expr" || declaration.Package == "datatable"
		if !declaration.Documented {
			if _, existing := backlog[key]; existing && !dsl {
				backlog[key] = true
			} else {
				t.Errorf("%s:%d: %s needs a doc comment; do not add documentation debt", declaration.File, declaration.Line, name)
			}
		}
		if dsl && (declaration.Kind == "func" || declaration.Kind == "type") && !declaration.Example {
			t.Errorf("%s:%d: %s needs an accurate standalone Example: line", declaration.File, declaration.Line, name)
		}
	}
	for key, stillUndocumented := range backlog {
		if !stillUndocumented {
			t.Errorf("remove documented or deleted declaration from documentation backlog: %s", key)
		}
	}
}

// TestConsumerAPIDSLGoDocSurface checks the documentation model used by go doc,
// so moving operations to functions must actually make them discoverable.
func TestConsumerAPIDSLGoDocSurface(t *testing.T) {
	declarations := consumerAPIDeclarations(t)
	for _, dir := range []string{"database/orm/expr", "datatable"} {
		positions := token.NewFileSet()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var files []*ast.File
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(positions, filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		pkg, err := doc.NewFromFiles(positions, files, "github.com/weiloon1234/Foundry-Go/"+dir)
		if err != nil {
			t.Fatal(err)
		}
		visible := make(map[string]bool)
		for _, function := range pkg.Funcs {
			visible[function.Name] = true
		}
		for _, typ := range pkg.Types {
			visible[typ.Name] = true
			for _, function := range typ.Funcs {
				visible[function.Name] = true
			}
			for _, method := range typ.Methods {
				visible[typ.Name+"."+method.Name] = true
			}
		}
		for _, declaration := range declarations {
			if declaration.Package != dir || declaration.Kind == "interface method" {
				continue
			}
			name := declaration.Name
			if declaration.Receiver != "" {
				name = declaration.Receiver + "." + name
			}
			if !visible[name] {
				t.Errorf("%s:%d: %s is hidden from go doc", declaration.File, declaration.Line, name)
			}
		}
	}
}
