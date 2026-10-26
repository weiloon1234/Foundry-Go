package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeType struct{}

func (fakeType) Underlying() types.Type { return fakeType{} }
func (fakeType) String() string         { return "fakeType" }

// Both recognition inputs, export-data package scopes and complete source
// checks, fail closed with the position, package and object being walked.
func TestPersistenceModelsFailsClosedOnUnhandledTypeKind(t *testing.T) {
	pkg := types.NewPackage("foundry.test/fake", "fake")
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "Unknown", fakeType{}))
	err := newPersistenceRecognizer(token.NewFileSet()).packageScope(pkg)
	if err == nil || !strings.Contains(err.Error(), "persistence-model recognition in foundry.test/fake (Unknown): unsupported go/types kind generate.fakeType") {
		t.Fatalf("expected unhandled type kind error from the package scope, got: %v", err)
	}
	fset := token.NewFileSet()
	file := fset.AddFile("fake.go", -1, 16)
	id := &ast.Ident{NamePos: file.Pos(4), Name: "Unknown"}
	source := collectPersistenceSource(pkg, &types.Info{Defs: map[*ast.Ident]types.Object{id: pkg.Scope().Lookup("Unknown")}})
	err = newPersistenceRecognizer(fset).source(source)
	if err == nil || !strings.Contains(err.Error(), "fake.go:1:5: persistence-model recognition in foundry.test/fake (Unknown): unsupported go/types kind generate.fakeType") {
		t.Fatalf("expected positioned unhandled type kind error from the source check, got: %v", err)
	}
}

func makeDAGChain() string {
	var b strings.Builder
	b.WriteString("type DAG1 lib.Pair[store.Table[Account, int], store.Table[Account, int]]\n")
	for i := 2; i <= 30; i++ {
		fmt.Fprintf(&b, "type DAG%d lib.Pair[DAG%d, DAG%d]\n", i, i-1, i-1)
	}
	b.WriteString("var Tables DAG30\n")
	return b.String()
}

func TestNativePersistenceModelsCannotBecomeTransportDTOs(t *testing.T) {
	define := `store.Define("accounts", func(b store.Builder[Account], a *Account) store.Table[Account,int] { return store.TableOf(b, &a.ID) })`
	for shape, tables := range map[string]string{
		// Exercises table handles wrapped in an embedded struct namespace.
		"namespace": `type accountTable struct { store.Table[Account,int] }
var Accounts = store.Define("accounts", func(b store.Builder[Account], a *Account) accountTable { return accountTable{store.TableOf(b, &a.ID)} })`,
		// Exercises slice element container traversal.
		"slice": "var Tables []store.Table[Account,int]",
		// Exercises array element container traversal.
		"array": "var Tables [1]store.Table[Account,int]",
		// Exercises map value container traversal.
		"map_value": "var Tables map[string][]store.Table[Account,int]",
		// Exercises map key container traversal.
		"map_key": "var Tables map[*store.Table[Account,int]]string",
		// Exercises pointer-to-slice indirection over table handle containers.
		"ptr_slice": "var Tables *[]store.Table[Account,int]",
		// Exercises generic type argument traversal on containers declared in a separate package.
		"generic_wrapper": "var Tables lib.Box[store.Table[Account,int]]",
		// Exercises non-generic cross-package structs containing table handles.
		"lib_holder": "var X lib.Holder",
		// Exercises methods on cross-package named types returning table handles.
		"cross_pkg_method": "var R lib.Repo",
		// Exercises type-parameter constraint interfaces returning table handles.
		"constraint": "type Box[T interface{ Get() store.Table[Account,int] }] struct{}",
		// Exercises recursive struct definitions carrying table handles.
		"recursive_node": `type Node struct {
	Next *Node
	T    store.Table[Account, int]
}
var Root Node`,
		// Exercises recursive interface definitions carrying table handles.
		"recursive_iface": `type I interface {
	M() I
	T() store.Table[Account, int]
}
var Root I`,
		// Exercises interface explicit method return signatures for table handles.
		"iface": "var Repo interface{ Accounts() store.Table[Account,int] }",
		// Exercises method signatures on named types returning table handles.
		"method": `type Repo struct{}
func (r Repo) Accounts() store.Table[Account,int] { return store.Table[Account,int]{} }`,
		// Exercises channel element type traversal carrying table handles.
		"chan": "var Tables chan store.Table[Account,int]",
		// Exercises package-level function result signatures returning table handles.
		"func_result": "func New() store.Table[Account,int] { return store.Table[Account,int]{} }",
		// Exercises unaliasing of type aliases resolving to table handles.
		"alias": `type T = store.Table[Account,int]
var X T`,
		// Exercises Define calls retaining table handles within slice initializers.
		"define_slice": "var Tables = []store.Table[Account,int]{" + define + "}",
		// Exercises Define calls erased behind interface containers in source packages.
		"define_erased": "var Tables = []any{" + define + "}",
		// Exercises shared type-argument DAGs without exponential traversal.
		"dag_chain": makeDAGChain(),
		// Exercises union term traversal in type-parameter constraints.
		"constraint_union": "type C interface{ ~[]store.Table[Account,int] }\ntype Box[T C] struct{}",
		// Exercises type-parameter constraint traversal on generic type aliases.
		"generic_alias": "type A[T interface{ Get() store.Table[Account,int] }] = []T",
		// Exercises type-parameter constraint traversal on generic functions.
		"func_constraint": "func F[T interface{ Get() store.Table[Account,int] }]() {}",
		// Exercises cross-package generic type instantiation with named constraint.
		"cross_pkg_box2": "var B lib.Box2[lib.Getter]",
		// Exercises local generic structs holding table handles bound by instantiation.
		"generic_holder": "type GenericHolder[M any] struct{ T store.Table[M,int] }\nvar H GenericHolder[Account]",
		// Exercises cross-package generic structs holding table handles bound by instantiation.
		"lib_generic_holder": "var H lib.GenericHolder[Account]",
		// Exercises local generic types with methods returning table handles bound by instantiation.
		"generic_method": "type GenericRepo[M any] struct{}\nfunc (GenericRepo[M]) Tab() store.Table[M,int] { return store.Table[M,int]{} }\nvar R GenericRepo[Account]",
		// Exercises cross-package generic types with methods returning table handles bound by instantiation.
		"lib_generic_method": "var R lib.GenericRepo[Account]",
		// Generic instances outside an explicit var type: a composite initializer,
		// a function body, a struct field, a function result, a non-erasing
		// wrapper function and a generic alias of the table handle.
		"generic_initializer":   "type GenericHolder[M any] struct{ T store.Table[M,int] }\nvar H = GenericHolder[Account]{}",
		"generic_function_body": "type GenericHolder[M any] struct{ T store.Table[M,int] }\nfunc init() { _ = GenericHolder[Account]{} }",
		"generic_struct_field":  "type GenericHolder[M any] struct{ T store.Table[M,int] }\ntype Repos struct{ Accounts GenericHolder[Account] }\nvar App Repos",
		"generic_result":        "type GenericHolder[M any] struct{ T store.Table[M,int] }\nfunc Tables() GenericHolder[Account] { return GenericHolder[Account]{} }",
		"wrapper_function":      "func Table[M any](string) store.Table[M,int] { return store.Table[M,int]{} }\nvar Accounts = Table[Account](\"accounts\")",
		"generic_alias_handle":  "type T[M any, K comparable] = store.Table[M,K]\ntype Ns struct{ T[Account,int] }\nvar Records Ns",
		// Generic wrappers whose instances expose no handle: a function, a
		// method of a generic type, a two-level chain declared outer-first and
		// an imported wrapper.
		"void_generic_function": "func Register[M any]() { var _ store.Table[M, int] }\nfunc init() { Register[Account]() }",
		"void_generic_method":   "type Repository[M any] struct{}\nfunc (Repository[M]) Init() { var _ store.Table[M, int] }\nvar _ = Repository[Account]{}",
		"void_wrapper_chain":    "func Outer[N any]() { Register[N]() }\nfunc Register[M any]() { var _ store.Table[M, int] }\nfunc init() { Outer[Account]() }",
		"lib_void_wrapper":      "func init() { lib.Register[Account]() }",
		// Wrapper forms: a real Define/TableOf call, an inferred instantiation,
		// a generic alias parameter, a chain across run packages and a model
		// parameter that is not the wrapper's first type parameter.
		"void_define_wrapper": "func Register[M any]() { _ = store.Define(\"rows\", func(b store.Builder[M], _ *M) store.Table[M, int] { var id int; return store.TableOf(b, &id) }) }\nfunc init() { Register[Account]() }",
		"inferred_wrapper":    "func Register[M any](M) { var _ store.Table[M, int] }\nfunc init() { Register(Account{}) }",
		"alias_wrapper":       "type A[M any] = store.Table[M, int]\nfunc Register[M any]() { var _ A[M] }\nfunc init() { Register[Account]() }",
		"lib_wrapper_chain":   "func Outer[N any]() { lib.Register[N]() }\nfunc init() { Outer[Account]() }",
		"permuted_wrapper":    "type Repo[M any] struct{}\nfunc (Repo[M]) Init() { var _ store.Table[M, int] }\ntype Wrap[D any, M any] struct{ Repo[M] }\nvar _ = Wrap[string, Account]{}",
		// A named table field beside a namespace's embedded handle.
		"named_table_field": `type Audit struct{ ID int }
type Namespace struct {
	store.Table[Audit, int]
	Accounts store.Table[Account, int]
}
var Records = store.Define("audits", func(b store.Builder[Audit], a *Audit) Namespace { return Namespace{Table: store.TableOf(b, &a.ID)} })`,
		// Exercises generic-alias origin deduplication when instantiated multiple times.
		"generic_alias_twice": `type Item1 struct{}
func (Item1) Get() store.Table[Account, int] { return store.Table[Account, int]{} }
type Item2 struct{}
func (Item2) Get() store.Table[Account, int] { return store.Table[Account, int]{} }
type AliasTwice[T interface{ Get() store.Table[Account,int] }] = []T
var X AliasTwice[Item1]
var Y AliasTwice[Item2]`,
	} {
		for _, imported := range []bool{false, true} {
			for name, field := range map[string]string{
				"field":    "Account Account",
				"embedded": "Account",
				"map_key":  "Values map[Account]string",
			} {
				t.Run(shape+"/"+name+map[bool]string{false: "_local", true: "_imported"}[imported], func(t *testing.T) {
					accountDecl := "type Account struct { ID int }"
					imports := `import store "github.com/weiloon1234/Foundry-Go/database/orm"`
					if shape == "lib_holder" || shape == "cross_pkg_method" || shape == "cross_pkg_box2" {
						imports = `import lib "foundry.test/generator/lib"`
						accountDecl = "type Account = lib.Account"
					} else if shape == "lib_generic_holder" || shape == "lib_generic_method" || shape == "lib_void_wrapper" || shape == "lib_wrapper_chain" {
						imports = `import lib "foundry.test/generator/lib"`
					} else if shape == "generic_wrapper" || shape == "dag_chain" {
						imports += "\nimport lib \"foundry.test/generator/lib\""
					}
					declaration := imports + "\n" + accountDecl + "\n" + tables + "\n"
					source := "package sample\n" + declaration + "//foundry:dto\ntype Response struct { " + field + " }\n"
					if imported {
						source = "package sample\nimport \"foundry.test/generator/domain\"\ntype Account = domain.Account\n//foundry:dto\ntype Response struct { " + field + " }\n"
					}
					dir := fixture(t, source)
					if shape == "generic_wrapper" || shape == "lib_holder" || shape == "cross_pkg_method" || shape == "dag_chain" || shape == "cross_pkg_box2" || shape == "lib_generic_holder" || shape == "lib_generic_method" || shape == "lib_void_wrapper" || shape == "lib_wrapper_chain" {
						if err := os.Mkdir(filepath.Join(dir, "lib"), 0755); err != nil {
							t.Fatal(err)
						}
						write(t, filepath.Join(dir, "lib"), "lib.go", `package lib
import store "github.com/weiloon1234/Foundry-Go/database/orm"
type Account struct { ID int }
type Box[T any] struct { V T }
type Holder struct { T store.Table[Account, int] }
type Repo struct{}
func (r Repo) Accounts() store.Table[Account, int] { return store.Table[Account, int]{} }
type Pair[A, B any] struct {
	First  *A
	Second *B
}
type Getter interface { Get() store.Table[Account, int] }
type Box2[T Getter] struct { V T }
type GenericHolder[M any] struct { T store.Table[M, int] }
type GenericRepo[M any] struct{}
func (GenericRepo[M]) Tab() store.Table[M, int] { return store.Table[M, int]{} }
func Register[M any]() { var _ store.Table[M, int] }
`)
					}
					if imported {
						if err := os.Mkdir(filepath.Join(dir, "domain"), 0755); err != nil {
							t.Fatal(err)
						}
						write(t, filepath.Join(dir, "domain"), "account.go", "package domain\n"+declaration)
					}
					_, err := Generate(t.Context(), Options{Dir: dir, Recursive: true})
					if err == nil || !strings.Contains(err.Error(), "persistence model") {
						t.Fatalf("native model accepted as %s: %v", name, err)
					}
					if len(generatedSnapshot(t, dir)) != 0 {
						t.Fatal("rejected model published transport output")
					}
				})
			}
		}
	}
}

func TestRetiredPersistenceDirectivesAreActionable(t *testing.T) {
	for _, kind := range []string{"model", "projection"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:"+kind+"\ntype Item struct {ID int}\n")
			_, err := Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "generation is retired") {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeTableEnumCapabilitiesWaitForGeneratedDescriptor(t *testing.T) {
	for _, shape := range []struct{ name, prefix string }{
		{"embedded", "orm."},
		{"dot_import", ""},
	} {
		t.Run(shape.name, func(t *testing.T) {
			source := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
//foundry:enum
type Status string
const Active Status = "active"
type Account struct {ID int; Status Status}
type Accounts struct {orm.Table[Account,int]; Status orm.InputEnum[Account,Status,Status]}
var Records = orm.Define("accounts",func(b orm.Builder[Account], m *Account) Accounts {
 return Accounts{orm.TableOf(b,&m.ID),orm.InputEnumOf[Account,Status](b,&m.Status,nil)}
})
//foundry:dto
type Response struct { Status Status }
`
			if shape.prefix == "" {
				source = strings.ReplaceAll(source, "orm.", "")
				source = strings.Replace(source, "import ", "import . ", 1)
			}
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Contract declarations must retain every original field during analysis, even
// when an embedded table handle resembles a persistence namespace.
func TestDTOWithTableHandleStillChecksItsOwnFields(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Account struct {ID int}
//foundry:dto
type Response struct { orm.Table[Account,int]; Callback func() }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "unsupported DTO field type") {
		t.Fatalf("DTO's unsupported callback field disappeared behind its table handle: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid DTO published output")
	}
}

func TestNestedDTOWithTableHandleRetainsCompleteShape(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained_field", true: "rejected_callback"}[unsupported], func(t *testing.T) {
			callback := ""
			if unsupported {
				callback = "; Callback func()"
			}
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Account struct {ID int}
type Payload struct { orm.Table[Account,int]; Label string `+"`json:\"label\"`"+callback+` }
type Envelope struct { Items []Payload }
//foundry:dto
type Response struct { Content Envelope }
`)
			_, err := Generate(t.Context(), Options{Dir: dir})
			if unsupported {
				if err == nil || !strings.Contains(err.Error(), "unsupported DTO field type") {
					t.Fatalf("nested unsupported field disappeared: %v", err)
				}
				if len(generatedSnapshot(t, dir)) != 0 {
					t.Fatal("invalid nested DTO published output")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if output := generatedSnapshot(t, dir)["response_foundry.gen.go"]; !strings.Contains(output, `Name: "label"`) {
				t.Fatalf("nested label missing from transport contract:\n%s", output)
			}
			if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A model declared by a sibling package must be rejected in every full run,
// regardless of which of the independent packages finishes first. A struct map
// key already fails discovery as an unsupported key; that package's import
// closure does not contain the sibling declaration, so it keeps that diagnostic.
func TestPersistenceModelsFromSiblingPackagesAreDeterministic(t *testing.T) {
	for name, shape := range map[string]struct{ field, diagnostic string }{
		"field":    {"Account models.Account", "persistence models cannot become DTO fields"},
		"embedded": {"models.Account", "persistence models cannot be embedded into DTOs"},
		"map_key":  {"Values map[models.Account]string", "DTO map keys"},
	} {
		field := shape.field
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\nimport \"foundry.test/generator/models\"\n//foundry:dto\ntype Response struct { "+field+" }\n")
			for directory, source := range map[string]string{
				"models": "package models\ntype Account struct { ID int }\n",
				"tables": `package tables
import (
	"foundry.test/generator/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)
var Accounts = orm.Define[models.Account]("accounts", func(b orm.Builder[models.Account], a *models.Account) orm.Table[models.Account, int] { return orm.TableOf(b, &a.ID) })
`,
			} {
				if err := os.Mkdir(filepath.Join(dir, directory), 0755); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(dir, directory), directory+".go", source)
			}
			var first string
			for run := 0; run < 5; run++ {
				_, err := Generate(t.Context(), Options{Dir: dir, Recursive: true})
				if err == nil || !strings.Contains(err.Error(), shape.diagnostic) {
					t.Fatalf("run %d accepted a sibling package's model as %s: %v", run, name, err)
				}
				if run == 0 {
					first = err.Error()
				} else if err.Error() != first {
					t.Fatalf("run %d reported %q, first run reported %q", run, err, first)
				}
				if len(generatedSnapshot(t, dir)) != 0 {
					t.Fatal("rejected model published transport output")
				}
			}
		})
	}
}

// Recognition must not pull initializers or method bodies into the declaration
// pass, where they can reference output that does not exist yet.
func TestPersistenceRecognitionKeepsValidPackagesGenerating(t *testing.T) {
	union := `//foundry:dto
type Card struct{ Token string ` + "`json:\"token\"`" + ` }
//foundry:dto
type Bank struct{ Reference string ` + "`json:\"reference\"`" + ` }
//foundry:union name=Payment discriminator=kind
type PaymentVariants struct{
 Card Card ` + "`union:\"card\"`" + `
 Bank Bank ` + "`union:\"bank\"`" + `
}
`
	for name, source := range map[string]string{
		"typed_generated_contract": `package sample
import "github.com/weiloon1234/Foundry-Go/contract"
//foundry:dto
type UserResponse struct{ Name string ` + "`json:\"name\"`" + ` }
var userContract contract.JSON[UserResponse] = UserResponseJSON()
`,
		"generated_kind_pointer": "package sample\nimport \"sync/atomic\"\n" + union + "var lastKind atomic.Pointer[PaymentKind]\n",
		"method_body_mentions_orm": "package sample\nimport \"github.com/weiloon1234/Foundry-Go/database/orm\"\n" + union + `type Account struct{ ID int }
func (Card) Last() PaymentKind { _ = orm.TableOf[Account, int]; return PaymentCardKind }
`,
		// Only the first type parameter of Service names a model, so the
		// second argument remains a valid DTO.
		"second_parameter_not_model": `package sample
import store "github.com/weiloon1234/Foundry-Go/database/orm"
type Account struct{ ID int }
type Service[M any, D any] struct{ last D }
func (Service[M, D]) Init() { var _ store.Table[M, int] }
//foundry:dto
type Summary struct{ Name string ` + "`json:\"name\"`" + ` }
var _ = Service[Account, Summary]{}
//foundry:dto
type Response struct{ Summary Summary ` + "`json:\"summary\"`" + ` }
`,
		// A wrapper instantiated with basic or interface arguments records
		// nothing instead of failing the run.
		"wrapper_basic_interface_arguments": `package sample
import store "github.com/weiloon1234/Foundry-Go/database/orm"
func Register[M any]() { var _ store.Table[M, int] }
func init() { Register[int](); Register[any]() }
//foundry:dto
type Response struct{ Name string ` + "`json:\"name\"`" + ` }
`,
		"typed_enum_capability": `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
//foundry:enum
type Status string
const Active Status = "active"
type Account struct {ID int; Status Status}
type Accounts struct {orm.Table[Account,int]; Status orm.InputEnum[Account,Status,Status]}
var Records = orm.Define("accounts",func(b orm.Builder[Account], m *Account) Accounts {
 return Accounts{orm.TableOf(b,&m.ID),orm.InputEnumOf[Account,Status](b,&m.Status,nil)}
})
var _ orm.InputEnum[Account,Status,Status] = Records.Status
//foundry:dto
type Response struct { Status Status }
`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A persistence handle whose model argument is not a named type is reported
// at its source position instead of being skipped.
func TestPersistenceModelArgumentsMustBeNamed(t *testing.T) {
	for name, declaration := range map[string]string{
		"pointer":   "var Tables store.Table[*Account, int]",
		"anonymous": "var Tables store.Table[struct{ ID int }, int]",
		"builder":   "func declare(store.Builder[[]Account]) {}",
		// Wrapper arguments that wrap a named type still fail closed.
		"wrapper_pointer": "func register[M any]() { var _ store.Table[M, int] }; func init() { register[*Account]() }",
		"wrapper_slice":   "func register[M any]() { var _ store.Table[M, int] }; func init() { register[[]Account]() }",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\nimport store \"github.com/weiloon1234/Foundry-Go/database/orm\"\ntype Account struct { ID int }\n"+declaration+"\n//foundry:dto\ntype Response struct { Name string }\n")
			_, err := Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "models.go:4:") || !strings.Contains(err.Error(), "persistence-model recognition in foundry.test/generator") || !strings.Contains(err.Error(), "is not a named type") {
				t.Fatalf("unnamed model argument was not diagnosed: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("rejected declaration published transport output")
			}
		})
	}
}

// When discovery fails inside a model, or on a model map key, the package's
// own complete source still names the persistence model, even though output
// it references does not exist yet.
func TestPersistenceModelDiagnosticPrecedesItsShape(t *testing.T) {
	for name, shape := range map[string]struct{ field, diagnostic, original string }{
		"field":    {"Account Account", "persistence models cannot become DTO fields", "unsupported DTO field type"},
		"embedded": {"Account", "persistence models cannot be embedded into DTOs", "unsupported DTO field type"},
		"map_key":  {"Values map[Account]string", "persistence models cannot become DTO map keys", "custom DTO map keys require a typed JSONKeyContract value method"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Account struct { ID int; Updates chan int }
func init() { _ = orm.Table[Account, int]{} }
var _ = ResponseJSON()
//foundry:dto
type Response struct { `+shape.field+` }
`)
			_, err := Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "models.go:7:6: "+shape.diagnostic) {
				t.Fatalf("model diagnostic did not take precedence: %v", err)
			}
			if !strings.Contains(err.Error(), "(discovery also failed: models.go:7:6: "+shape.original) {
				t.Fatalf("refined diagnostic dropped the discovery failure: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("rejected model published transport output")
			}
		})
	}
}
