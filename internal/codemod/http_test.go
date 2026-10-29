package codemod

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Frozen declarations deliberately do not import the live framework HTTP API.
const oldHTTP = `package http
 type Endpoint[P,Q,B,R any] struct{}
 type Route[P any] struct{}
 type Scope struct{}
 func (s Scope) Within(Scope) Scope { return s }
 func DefineEndpoint[P,Q,B,R any](r Route[P],q Q,b B,response R) Endpoint[P,Q,B,R] { return Endpoint[P,Q,B,R]{} }
 func DefineRoute[P any](p P) Route[P] { return Route[P]{} }
 func (e Endpoint[P,Q,B,R]) Within(Scope) Endpoint[P,Q,B,R] { return e }
 func (e Endpoint[P,Q,B,R]) WithTimeout(int) Endpoint[P,Q,B,R] { return e }
 func (e Endpoint[P,Q,B,R]) WithMiddleware(...int) Endpoint[P,Q,B,R] { return e }
 func (e Endpoint[P,Q,B,R]) WithBodyValidation(...B) Endpoint[P,Q,B,R] { return e }
 func (e Endpoint[P,Q,B,R]) Validate() error { return nil }
 func (e Endpoint[P,Q,B,R]) ID() string { return "" }
 func (e Endpoint[P,Q,B,R]) Handle(int) int { return 0 }
 func (r Route[P]) Within(Scope) Route[P] { return r }
 func (r Route[P]) Validate() error { return nil }
 func (r Route[P]) ID() string { return "" }
 type AuthenticatedEndpoint[P,Q,B,M,R any] struct{}
 func Authenticated[P,Q,B,M,R any](e Endpoint[P,Q,B,R],m M) AuthenticatedEndpoint[P,Q,B,M,R] { return AuthenticatedEndpoint[P,Q,B,M,R]{} }
 func (a AuthenticatedEndpoint[P,Q,B,M,R]) WithActorMiddleware(...int) AuthenticatedEndpoint[P,Q,B,M,R] { return a }
 func (a AuthenticatedEndpoint[P,Q,B,M,R]) WithPermissions(...M) AuthenticatedEndpoint[P,Q,B,M,R] { return a }
 func (a AuthenticatedEndpoint[P,Q,B,M,R]) Validate() error { return nil }
`
const oldHTTPSpecialized = `
 type SignedEndpoint[P,Q,B,R any] struct{}
 func (e SignedEndpoint[P,Q,B,R]) Validate() error { return nil }
 type OptionalAuthenticationEndpoint[P,Q,B,M,R any] struct{}
 func (e OptionalAuthenticationEndpoint[P,Q,B,M,R]) Validate() error { return nil }
 type SignedAuthenticatedEndpoint[P,Q,B,S,R any] struct{}
 func (e SignedAuthenticatedEndpoint[P,Q,B,S,R]) Validate() error { return nil }
`
const oldBinding = `package modelbinding
 type Transport interface { Validate() error }
 func Bind(e Transport, resolver int) int { return 0 }
 func BindAuthenticated(e Transport, resolver int) int { return 0 }
`

type httpTestImporter map[string]*types.Package

func (i httpTestImporter) Import(path string) (*types.Package, error) {
	if pkg := i[path]; pkg != nil {
		return pkg, nil
	}
	return nil, fmt.Errorf("unexpected import %s", path)
}

func rewriteHTTPTest(t *testing.T, source, name string, dependencies map[string]string) ([]byte, []Entry) {
	t.Helper()
	fset := token.NewFileSet()
	importer := httpTestImporter{}
	for path, source := range dependencies {
		file, err := parser.ParseFile(fset, path+".go", source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		config := types.Config{}
		pkg, err := config.Check(path, fset, []*ast.File{file}, nil)
		if err != nil {
			t.Fatal(err)
		}
		importer[path] = pkg
	}
	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	info := newInfo()
	config := types.Config{Importer: importer}
	pkg, err := config.Check("consumer", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}
	output, entries, err := rewriteFile(fset, file, info, pkg, []RuleSet{HTTP{}})
	if err != nil {
		t.Fatal(err)
	}
	return output, entries
}

func TestHTTPGolden(t *testing.T) {
	old, err := os.ReadFile("testdata/http.old.go")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/http.new.go")
	if err != nil {
		t.Fatal(err)
	}
	expected, err = format.Source(expected)
	if err != nil {
		t.Fatal(err)
	}
	output, entries := rewriteHTTPTest(t, string(old), "consumer.go", map[string]string{httpPath: oldHTTP, modelbindingPath: oldBinding})
	if !bytes.Equal(output, expected) {
		t.Fatalf("got:\n%s\nwant:\n%s", output, expected)
	}
	if len(entries) == 0 {
		t.Fatal("no HTTP findings")
	}
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
}

func TestHTTPUnresolved(t *testing.T) {
	for _, body := range []string{
		`func use(e *h.Endpoint[int,int,int,int]) { _ = e.Validate() }`,
		`type Wrapped struct { h.Endpoint[int,int,int,int] }; func use(e Wrapped) { _ = e.Validate() }`,
		`func use(e h.Endpoint[int,int,int,int]) { h := 1; _ = h; _ = e.Validate() }`,
		`var _ = h.Endpoint[int,int,int,int].WithTimeout`,
		`var _ = (h.Endpoint[int,int,int,int]{}).WithTimeout`,
		`func side() int { return 0 }; var _ = (h.Endpoint[int,int,int,int]{}).WithTimeout(side())`,
		`var _ interface{Validate() error} = h.Endpoint[int,int,int,int]{}`,
		`func use(e h.Endpoint[int,int,int,int]) interface{Validate() error} { return e }`,
		`func sink(interface{Validate() error}) {};func use(e h.Endpoint[int,int,int,int]) { sink(e) }`,
		`var _ = []interface{Validate() error}{h.Endpoint[int,int,int,int]{}}`,
		`func use[T interface{Validate() error}](e T) {};var _ = use[h.Endpoint[int,int,int,int]]`,
	} {
		t.Run(body, func(t *testing.T) {
			source := "package consumer\nimport h \"" + httpPath + "\"\n" + body
			output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{httpPath: oldHTTP})
			if len(entries) != 1 || entries[0].Status != Unresolved {
				t.Fatal(entries)
			}
			if string(output) != source {
				t.Fatal("unresolved source changed")
			}
		})
	}
}

func TestHTTPCapture(t *testing.T) {
	source := `package consumer
 import h "github.com/weiloon1234/Foundry-Go/http"
 func next() h.Endpoint[int,int,int,int] { return h.Endpoint[int,int,int,int]{} }
 var validate = next().Validate
 var endpoint h.Endpoint[int,int,int,int]
 func mutate() int { endpoint = next(); return 1 }
 var _ = endpoint.WithBodyValidation(mutate())
 `
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{httpPath: oldHTTP})
	if len(entries) != 2 {
		t.Fatal(entries)
	}
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	if strings.Count(string(output), "next()") != 3 {
		t.Fatalf("receiver duplicated: %s", output)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "output.go", output, 0)
	if err != nil {
		t.Fatal(err)
	}
	captures := 0
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if _, ok := call.Fun.(*ast.FuncLit); ok {
				captures++
				if len(call.Args) != 1 {
					t.Fatal("capture must evaluate receiver once")
				}
			}
		}
		return true
	})
	if captures != 2 {
		t.Fatalf("missing captures: %s", output)
	}
}

func TestHTTPGenerated(t *testing.T) {
	for _, name := range []string{"endpoint_foundry.gen.go", "endpoint.go"} {
		source := "// Code generated by foundry. DO NOT EDIT.\npackage consumer\nimport h \"" + httpPath + "\"\nvar _ = (h.Endpoint[int,int,int,int]{}).Validate()\n"
		output, entries := rewriteHTTPTest(t, source, name, map[string]string{httpPath: oldHTTP})
		if string(output) != source || len(entries) != 1 || entries[0].Status != Regenerate {
			t.Fatal(entries)
		}
	}
}

func TestHTTPCustomBinding(t *testing.T) {
	source := `package consumer
 import b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
 type Custom struct{}
 func (Custom) Validate() error { return nil }
 var _ = b.Bind(Custom{}, 0)
 `
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{modelbindingPath: oldBinding})
	if string(output) != source || len(entries) != 1 || entries[0].Status != Unresolved {
		t.Fatal(entries)
	}
}

// These goldens are frozen from the approved appendix independently of the
// runtime mapping table. Simplified signatures isolate name/family selection.
func TestHTTPAppendixMethods(t *testing.T) {
	for _, family := range []struct{ name, path string }{{"core", httpPath}, {"pagination", paginationPath}, {"modelbinding", modelbindingPath}} {
		t.Run(family.name, func(t *testing.T) {
			read := func(suffix string) string {
				data, err := os.ReadFile("testdata/http_" + family.name + suffix + ".go")
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			dependencies := map[string]string{family.path: read("_v01")}
			if family.path == paginationPath {
				dependencies[httpPath] = oldHTTP
			}
			output, entries := rewriteHTTPTest(t, read("_methods.old"), "consumer.go", dependencies)
			expected, err := format.Source([]byte(read("_methods.new")))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(output, expected) {
				t.Fatalf("got:\n%s\nwant:\n%s", output, expected)
			}
			for _, entry := range entries {
				if entry.Status != Resolved {
					t.Fatal(entry)
				}
			}
		})
	}
}

func TestHTTPBindingFamilies(t *testing.T) {
	source := `package consumer
 import h "github.com/weiloon1234/Foundry-Go/http"
 import b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
 import v "github.com/weiloon1234/Foundry-Go/value"
 var _ = b.Bind(h.Endpoint[int,int,int,int]{}, 0)
 var _ = b.Bind(h.SignedEndpoint[int,int,int,int]{}, 0)
 var _ = b.BindAuthenticated(h.AuthenticatedEndpoint[int,int,int,string,int]{}, 0)
 var _ = b.BindAuthenticated(h.OptionalAuthenticationEndpoint[int,int,int,string,int]{}, 0)
 var _ = b.BindAuthenticated(h.SignedAuthenticatedEndpoint[int,int,int,string,int]{}, 0)
 var _ = b.BindAuthenticated(h.SignedAuthenticatedEndpoint[int,int,int,v.Optional[string],int]{}, 0)
 `
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{httpPath: oldHTTP + oldHTTPSpecialized, modelbindingPath: oldBinding, "github.com/weiloon1234/Foundry-Go/value": "package value; type Optional[T any] struct{}"})
	if len(entries) != 6 {
		t.Fatal(entries)
	}
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	for _, adapter := range []string{"ForBinding", "ForSignedBinding", "ForAuthenticatedBinding", "ForOptionalBinding", "ForSignedAuthenticatedBinding", "ForSignedOptionalBinding"} {
		if !strings.Contains(string(output), "h."+adapter+"(") {
			t.Fatalf("missing %s: %s", adapter, output)
		}
	}
}

func TestHTTPDotImport(t *testing.T) {
	source := "package consumer\nimport . \"" + httpPath + "\"\nvar _ = (Endpoint[int,int,int,int]{}).Validate()\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{httpPath: oldHTTP})
	if string(output) != source || len(entries) != 1 || entries[0].Status != Unresolved {
		t.Fatal(entries)
	}
}

func TestHTTPUnrelatedPackage(t *testing.T) {
	source := "package consumer\nimport h \"example.org/http\"\nvar _ = (h.Endpoint[int,int,int,int]{}).Validate()\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{"example.org/http": oldHTTP})
	if string(output) != source || len(entries) != 0 {
		t.Fatal(entries)
	}
}

func TestHTTPStoredBindingConstructor(t *testing.T) {
	source := "package consumer\nimport b \"" + modelbindingPath + "\"\nvar bind = b.Bind\n"
	output, entries := rewriteHTTPTest(t, source, "consumer.go", map[string]string{modelbindingPath: oldBinding})
	if string(output) != source || len(entries) != 1 || entries[0].Status != Unresolved {
		t.Fatal(entries)
	}
}
