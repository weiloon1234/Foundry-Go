package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const pathSource = `package sample
import (
    foundryhttp "github.com/weiloon1234/Foundry-Go/http"
    "github.com/weiloon1234/Foundry-Go/model"
    "github.com/weiloon1234/Foundry-Go/decimal"
    "github.com/weiloon1234/Foundry-Go/temporal"
)
type Member struct{}
type Code string
type path int16
type Flag bool
//foundry:enum
type State string
const Active State = "active"
//foundry:path pattern=/members/{member}/{code}/{revision}/{active}/{state}/{date}/{amount}
type MemberPath struct {
    Member model.ID[Member]
    Code Code
    Revision path
    Active Flag
    State State
    Date temporal.Date
    Amount decimal.Decimal
    Ignored map[string]any ` + "`path:\"-\"`" + `
}
//foundry:path pattern=/health
type HealthPath struct{}
//foundry:path pattern=/assets/{remaining...}
type AssetPath struct { Remaining string }
var ShowMember = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID:"members.show",Method:foundryhttp.GET,Access:foundryhttp.Public},MemberPathDescriptor())
func URL(input MemberPath)(string,error) { return ShowMember.URL(input) }
`

func TestFreshPathGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, pathSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh path stale check: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("check wrote output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["member_path_foundry.gen.go"]
	for _, want := range []string{"func MemberPathDescriptor()", "Path[MemberPath]", "ModelIDPath[Member]", "StringPath[Code]", "IntegerPath[path]", "BoolPath[Flag]", "EnumPath[State, *State]", "TextPath[temporal.Date, *temporal.Date]", "TextPath[decimal.Decimal, *decimal.Decimal]", "path1 *MemberPath"} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated path missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Ignored") || strings.Contains(output, "Draft") || strings.Contains(output, dir) {
		t.Fatal("path output acquired skipped fields, persistence behavior or absolute source paths")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("path generation was not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "path_runtime_test.go", `package sample
import (
 "net/http"
 "net/http/httptest"
 "testing"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/temporal"
)
func TestGeneratedRoute(t *testing.T) {
 metadata,err:=MemberPathDescriptor().Parameters();if err!=nil{t.Fatal(err)}
 if len(metadata)!=7{t.Fatal("fresh path metadata missing")}
 for _,parameter:=range metadata {if parameter.Scalar==nil{t.Fatal("fresh path scalar missing",parameter.Name)}}
 if metadata[4].Scalar.Syntax!=foundryhttp.EnumURLSyntax || len(metadata[4].Scalar.Value.Cases)!=1{t.Fatal("fresh enum metadata missing")}
 id,err:=model.NewID[Member]();if err!=nil{t.Fatal(err)}
 date,err:=temporal.ParseDate("2026-09-14");if err!=nil{t.Fatal(err)}
 amount,err:=decimal.Parse("12345678901234567890.123456789");if err!=nil{t.Fatal(err)}
 input:=MemberPath{Member:id,Code:"a/b",Revision:17,Active:true,State:Active,Date:date,Amount:amount}
 location,err:=URL(input);if err!=nil{t.Fatal(err)}
 var got MemberPath
 router,err:=foundryhttp.NewRouter(ShowMember.HandleRaw(func(w http.ResponseWriter,_ *http.Request,p MemberPath){got=p;w.WriteHeader(204)}));if err!=nil{t.Fatal(err)}
 recorder:=httptest.NewRecorder();router.ServeHTTP(recorder,httptest.NewRequest("GET",location,nil))
 if recorder.Code!=204 || got.Member!=id || got.Code!=input.Code || got.Revision!=17 || !got.Active || got.State!=Active || got.Date.String()!=date.String() || got.Amount.String()!=amount.String(){t.Fatal("generated route lost typed values")}
 input.State=State("undeclared");if _,err:=URL(input);err==nil{t.Fatal("generated enum path bypassed membership")}
 if location,err:=HealthPathDescriptor().URL(HealthPath{});err!=nil||location!="/health"{t.Fatalf("static generated path: %q %v",location,err)}
 if location,err:=AssetPathDescriptor().URL(AssetPath{Remaining:"images/a b.png"});err!=nil||location!="/assets/images/a%20b.png"{t.Fatalf("catch-all generated path: %q %v",location,err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated public route did not run: %v\n%s", err, output)
	}
}

func TestInvalidPathDeclarationsDoNotPublish(t *testing.T) {
	for name, source := range map[string]string{
		"missing pattern":       "//foundry:path\ntype Input struct{}",
		"nonstruct":             "//foundry:path pattern=/x\ntype Input string",
		"unbound":               "//foundry:path pattern=/{id}\ntype Input struct{}",
		"unused field":          "//foundry:path pattern=/x\ntype Input struct{ ID string }",
		"pointer":               "//foundry:path pattern=/{id}\ntype Input struct{ ID *string }",
		"interface":             "//foundry:path pattern=/{id}\ntype Input struct{ ID any }",
		"unexported":            "//foundry:path pattern=/{id}\ntype Input struct{ id string }",
		"embedded":              "type ID struct{}\n//foundry:path pattern=/{id}\ntype Input struct{ ID }",
		"duplicate binding":     "//foundry:path pattern=/{id}\ntype Input struct{ ID string; Other string `path:\"id\"` }",
		"bad tag":               "//foundry:path pattern=/{id}\ntype Input struct{ ID string `path:\"\"` }",
		"duplicate tag":         "//foundry:path pattern=/{id}\ntype Input struct{ ID string `path:\"id\" path:\"other\"` }",
		"persistence":           "//foundry:path pattern=/{id}\ntype Input struct{ ID string `foundry:\"column=id\"` }",
		"bad pattern":           "//foundry:path pattern=/{id...}/tail\ntype Input struct{ ID string }",
		"duplicate parameter":   "//foundry:path pattern=/{id}/{id}\ntype Input struct{ ID string }",
		"symbol collision":      "//foundry:path pattern=/{id}\ntype Input struct{ ID string }\nfunc InputDescriptor(){}",
		"partial codec":         "type Code string\nfunc(Code)MarshalText()([]byte,error){return nil,nil}\n//foundry:path pattern=/{id}\ntype Input struct{ ID Code }",
		"wrong codec signature": "type Code string\nfunc(Code)MarshalText()string{return \"\"}\n//foundry:path pattern=/{id}\ntype Input struct{ ID Code }",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid path generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid path published output")
			}
		})
	}
}

func TestPathFieldChangesProduceStaleOutput(t *testing.T) {
	source := "package sample\n//foundry:path pattern=/{member}\ntype Input struct{ Member string }"
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	updated := "package sample\n//foundry:path pattern=/{member}\ntype Input struct{ Key string `path:\"member\"` }"
	write(t, dir, "models.go", updated)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("renamed binding was not stale: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("stale check changed generated output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generatedSnapshot(t, dir)["input_foundry.gen.go"], ".Key") {
		t.Fatal("renamed typed selector was not regenerated")
	}
}

func TestPathTextCodecDiscoveryOnFreshCheckout(t *testing.T) {
	dir := fixture(t, `package sample
type Token[S ~string] struct{ Value S }
func (v Token[S]) MarshalText() ([]byte,error) {
    _ = InputDescriptor // Generated references stay valid in handwritten methods.
    return []byte(v.Value),nil
}
func (v *Token[S]) UnmarshalText(data []byte) error { v.Value=S(data);return nil }
type Label = Token[string]
//foundry:path pattern=/{label}
type Input struct { Label Label }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["input_foundry.gen.go"]
	if !strings.Contains(output, "TextPath[Label, *Label]") {
		t.Fatalf("generic alias lost its text codec: %s", output)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
