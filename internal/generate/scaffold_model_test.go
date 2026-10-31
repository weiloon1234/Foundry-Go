package generate

import (
	"bytes"
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateScaffoldGoldens = flag.Bool("update-scaffold-goldens", false, "refresh handwritten ORM scaffold golden output")

func TestORMScaffoldSymbolCollisions(t *testing.T) {
	cases := []struct {
		name    string
		options ScaffoldOptions
		want    []string
	}{
		{"table_type", ScaffoldOptions{Name: "Widget", Table: "widget_table"}, []string{"var WidgetTable1 =", "type WidgetTable struct"}},
		{"table_helper", ScaffoldOptions{Name: "Widget", Table: "widget_tables"}, []string{"var WidgetTables1 =", "func WidgetTables()"}},
		{"qualified_table", ScaffoldOptions{Name: "Widget", Table: "public.widget_table"}, []string{"var WidgetTable1 ="}},
		{"empty_namespace", ScaffoldOptions{Name: "Widget", Table: "_"}, []string{"var Query ="}},
		{"digit_namespace", ScaffoldOptions{Name: "Widget", Table: "_9"}, []string{"var Query9 ="}},
		{"qualified_empty_namespace", ScaffoldOptions{Name: "Widget", Table: "public._"}, []string{"var Query ="}},
		{"qualified_digit_namespace", ScaffoldOptions{Name: "Widget", Table: "public._9"}, []string{"var Query9 ="}},
		{"digits_namespace", ScaffoldOptions{Name: "Widget", Table: "__123"}, []string{"var Query123 ="}},
		{"qualified_digits_namespace", ScaffoldOptions{Name: "Widget", Table: "public.__123"}, []string{"var Query123 ="}},
		{"keyword_namespace", ScaffoldOptions{Name: "Widget", Table: "type"}, []string{"var Type ="}},
		{"qualified_keyword_namespace", ScaffoldOptions{Name: "Widget", Table: "public.type"}, []string{"var Type ="}},
		{"predeclared_namespace", ScaffoldOptions{Name: "Widget", Table: "int"}, []string{"var Int ="}},
		{"qualified_predeclared_namespace", ScaffoldOptions{Name: "Widget", Table: "public.nil"}, []string{"var Nil ="}},
		{"unicode_model", ScaffoldOptions{Name: "Éclair", Table: "_9"}, []string{"var Query9 ="}},
		{"qualified_unicode_model", ScaffoldOptions{Name: "Éclair", Table: "public._"}, []string{"var Query ="}},
		{"fallback_collision", ScaffoldOptions{Name: "Query9", Table: "_9"}, []string{"var Query91 ="}},
		{"qualified_fallback_collision", ScaffoldOptions{Name: "Query9", Table: "public._9"}, []string{"var Query91 ="}},
		{"metadata_type", ScaffoldOptions{Name: "Article", Table: "articles", Metadata: []string{"Table"}}, []string{"type ArticleTable1 struct{}"}},
		{"structural_family", ScaffoldOptions{Name: "Article", Table: "article_extensions", Attachment: []string{"Logo"}, Disk: "public", Metadata: []string{"Table", "Table1", "Tables", "Disk", "ExtensionOwner", "ExtensionSlots", "Extensions", "Extensions1", "ExtensionDeclaration"}}, []string{"type ArticleTable2 struct{}", "type ArticleTable1 struct{}", "var ArticleExtensions2 =", "type ArticleExtensions3 struct{}"}},
		{"codec_namespace", ScaffoldOptions{Name: "Article", Table: "article_seo_json", Metadata: []string{"SEO"}}, []string{"var ArticleSeoJson =", "type ArticleSEO struct{}"}},
		{"codec_and_validation_families", ScaffoldOptions{Name: "Article", Table: "article_foo_json", Metadata: []string{"Foo", "FooJSON", "FooJSON1", "FooValidationFields", "FooValidationFieldSet"}}, []string{"type ArticleFooJSON2 struct{}", "type ArticleFooJSON1 struct{}", "type ArticleFooValidationFields1 struct{}", "type ArticleFooValidationFieldSet1 struct{}"}},
		{"exact_codec_namespace", ScaffoldOptions{Name: "Article", Table: "article_foo_j_s_o_n", Metadata: []string{"Foo", "Foo1"}}, []string{"var ArticleFooJSON =", "type ArticleFoo2 struct{}", "type ArticleFoo1 struct{}"}},
	}
	baseDir := fixture(t, "package fixture\n")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.options
			o.Kind = ModelScaffold
			first, err := renderScaffold(tc.name, o)
			if err != nil {
				t.Fatal(err)
			}
			second, err := renderScaffold(tc.name, o)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("nondeterministic scaffold", err)
			}
			for _, want := range tc.want {
				if !bytes.Contains(first, []byte(want)) {
					t.Fatalf("missing %q in scaffold:\n%s", want, first)
				}
			}
			pkgDir := filepath.Join(baseDir, tc.name)
			if err := os.Mkdir(pkgDir, 0755); err != nil {
				t.Fatal(err)
			}
			write(t, pkgDir, "doc.go", "package "+tc.name+"\n")
			o.Dir = pkgDir
			if _, err := Scaffold(t.Context(), o); err != nil {
				t.Fatal(err)
			}
			write(t, pkgDir, "scaffold_test.go", fmt.Sprintf(`package %s
import ("testing"; "github.com/weiloon1234/Foundry-Go/database/orm")
func TestDeclaration(t *testing.T) {
 if err := orm.Check(%sTables()...); err != nil { t.Fatal(err) }
}
`, tc.name, o.Name))
		})
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	cmd.Dir = baseDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("batched scaffold compilation and orm.Check: %v\n%s", err, output)
	}
}

// SQL grammar remains narrower than Go identifiers; unsupported names must not
// reach publication while valid Unicode Go model names compile above.
func TestORMScaffoldRejectsInvalidSQLNames(t *testing.T) {
	for _, table := range []string{"9", "123", "public.9", "public.123", "用户", "public.用户", "éclair", "public.éclair"} {
		t.Run(table, func(t *testing.T) {
			dir := fixture(t, "package sample\n")
			_, err := Scaffold(t.Context(), ScaffoldOptions{Dir: dir, Kind: ModelScaffold, Name: "Widget", Table: table})
			if err == nil || !strings.Contains(err.Error(), "requires --table") {
				t.Fatalf("invalid SQL table: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "widget_model.go")); !os.IsNotExist(err) {
				t.Fatalf("invalid scaffold published: %v", err)
			}
		})
	}
}

func TestScaffoldIdentifierGrammar(t *testing.T) {
	invalid := append([]string{"", "_", "9", "9Widget", "has-dash", "has space", "😀", "\xff"}, types.Universe.Names()...)
	for keyword := token.BREAK; keyword <= token.VAR; keyword++ {
		invalid = append(invalid, keyword.String())
	}
	for _, name := range invalid {
		if validScaffoldIdentifier(name) {
			t.Errorf("accepted unusable identifier %q", name)
		}
	}
	for _, name := range []string{"Query", "Query9", "Widget", "Type", "Int", "Éclair", "模型", "Widget９"} {
		if !validScaffoldIdentifier(name) {
			t.Errorf("rejected valid identifier %q", name)
		}
	}
}

func TestORMScaffoldGoldens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options ScaffoldOptions
	}{
		{"model", ScaffoldOptions{Kind: ModelScaffold, Name: "Widget", Table: "widgets"}},
		{"extensions", ScaffoldOptions{Kind: ModelScaffold, Name: "Article", Table: "articles", Translated: []string{"Title"}, Attachment: []string{"Logo"}, Attachments: []string{"Gallery"}, Metadata: []string{"SEO"}, Disk: "public"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderScaffold("models", tc.options)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "scaffold", tc.name+".golden")
			if *updateScaffoldGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("scaffold differs from %s; refresh with -update-scaffold-goldens", path)
			}
			if strings.Contains(string(got), "//foundry:model") || strings.Contains(string(got), "database/query") {
				t.Fatal("scaffold retained legacy model generation")
			}
		})
	}
}

// Adding fields to the scaffold remains a handwritten operation. Exercise the
// guide's managed-role, separate-input and relation additions on the actual output.
func TestORMScaffoldHandwrittenRolesAndRelations(t *testing.T) {
	dir := fixture(t, "package sample\n")
	path, err := Scaffold(t.Context(), ScaffoldOptions{Dir: dir, Kind: ModelScaffold, Name: "Widget", Table: "widget"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, edit := range [][2]string{
		{"import (", `import (
 "strings"
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/database/relation"`},
		{"type Widget struct {", `type Widget struct {
 Name string
 CreatedAt temporal.DateTime
 UpdatedAt temporal.DateTime
 DeletedAt value.Nullable[temporal.DateTime]
 Children relation.Many[Child]`},
		{"type WidgetTable struct {", `type WidgetTable struct {
 Name orm.InputText[Widget,NameInput,string]
 CreatedAt orm.CreatedAt[Widget,temporal.DateTime]
 UpdatedAt orm.UpdatedAt[Widget,temporal.DateTime]
 DeletedAt orm.SoftDelete[Widget,temporal.DateTime]
 Soft orm.SoftDeletable[Widget]
 Children orm.Many[Widget,Child]`},
		{"return WidgetTable{", `deleted:=orm.SoftDeleteOf(b,&m.DeletedAt)
 return WidgetTable{
 Name:orm.InputTextOf(b,&m.Name,func(input NameInput)(string,error){return strings.TrimSpace(input.Text),nil}),
 CreatedAt:orm.CreatedAtOf(b,&m.CreatedAt),UpdatedAt:orm.UpdatedAtOf(b,&m.UpdatedAt),
 DeletedAt:deleted,Soft:orm.SoftDeletes(deleted),Children:orm.ManyOf(b,&m.Children),`},
	} {
		if !strings.Contains(source, edit[0]) {
			t.Fatal("scaffold edit anchor missing", edit[0])
		}
		source = strings.Replace(source, edit[0], edit[1], 1)
	}
	source += `
 type NameInput struct{Text string}
 type Child struct{ID model.ID[Child];WidgetID model.ID[Widget]}
 type ChildTable struct{orm.Table[Child,model.ID[Child]];WidgetID orm.Column[Child,model.ID[Widget]]}
 var Children=orm.Define("children",func(b orm.Builder[Child],m *Child)ChildTable{return ChildTable{orm.TableOf(b,&m.ID),orm.ColumnOf(b,&m.WidgetID)}})
 var WidgetChildren=orm.HasManyOf(Widgets.Children,orm.KeyExpr(Widgets.Key()),orm.ColumnExpr(Children.WidgetID))
 `
	write(t, dir, filepath.Base(path), source)
	write(t, dir, "roles_test.go", `package sample
 import("testing";"github.com/weiloon1234/Foundry-Go/database/orm")
 func TestRoles(t *testing.T){
  if err:=orm.Check(append(WidgetTables(),Children)...);err!=nil{t.Fatal(err)}
  if _,err:=orm.Compile(Widgets.Query().With(WidgetChildren));err!=nil{t.Fatal(err)}
  _=orm.InputTextSet(Widgets.Name,NameInput{Text:"  normalized  "})
 }
 `)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != source {
		t.Fatal("generation rewrote handwritten role declarations", err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("handwritten role additions: %s %v", output, err)
	}
}
