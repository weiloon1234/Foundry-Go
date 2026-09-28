package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const dtoSource = `package sample
import (
 "encoding/json"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/temporal"
)
type Member struct{}
//foundry:enum
type State string
const Active State = "active"
//foundry:dto
type MemberDTO struct {
 ID model.ID[Member] ` + "`json:\"id\"`" + `
 State State ` + "`json:\"state\"`" + `
 Email value.Optional[value.Nullable[string]] ` + "`json:\"email,omitzero\"`" + `
 Children []MemberDTO ` + "`json:\"children,omitempty\"`" + `
 Counts map[string]uint16 ` + "`json:\"counts,omitempty\"`" + `
 Sequence *uint32 ` + "`json:\"sequence,string,omitempty\"`" + `
 Flags [2]bool ` + "`json:\"flags,omitempty\"`" + `
 Bytes []byte ` + "`json:\"bytes,omitempty\"`" + `
 Amount value.Optional[decimal.Decimal] ` + "`json:\"amount,omitzero\"`" + `
 Date value.Optional[temporal.Date] ` + "`json:\"date,omitzero\"`" + `
 Exact json.Number ` + "`json:\"exact,omitempty\"`" + `
 Ignore func() ` + "`json:\"-\"`" + `
}
var memberWire = MemberDTOJSON()
`

func TestFreshDTOGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, dtoSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh DTO stale check: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("stale check wrote files")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["member_dto_foundry.gen.go"]
	for _, want := range []string{"func MemberDTOJSON()", "JSON[MemberDTO]", "DefineJSON[MemberDTO]", "EnumType", ".EnumDescriptor()", `Name: "email"`, `Name: "sequence"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("DTO output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"Ignore", "Draft", "QueryMembers", dir} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("DTO output acquired %q", unwanted)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("DTO generation was not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "dto_runtime_test.go", `package sample_test
import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestPublicGeneratedDTO(t *testing.T) {
 descriptor:=sample.MemberDTOJSON()
 if err:=descriptor.Validate();err!=nil{t.Fatal(err)}
 limits:=contract.JSONLimits{Bytes:4096,Depth:16,Nodes:500,Steps:2000,Issues:20}
 input:=[]byte("{\"id\":\"0193fd8c-2075-7000-8000-000000000001\",\"state\":\"active\",\"email\":null,\"sequence\":\"42\",\"flags\":[true,false],\"bytes\":\"YQ==\",\"counts\":{\"a\":65535},\"exact\":1e10000,\"amount\":\"12345678901234567890.001\",\"date\":\"2024-02-29\"}")
 result,err:=descriptor.Decode(context.Background(),input,limits);if err!=nil{t.Fatal(err)}
 email,present:=result.Email.Get()
 if !present || !email.IsNull() || result.State!=sample.Active || result.Sequence==nil || *result.Sequence!=42 || string(result.Bytes)!="a" || result.Counts["a"]!=65535 || result.Exact!=json.Number("1e10000"){t.Fatal("DTO values lost their concrete representation")}
 amount,present:=result.Amount.Get();if !present || amount.String()!="12345678901234567890.001"{t.Fatal("exact decimal changed")}
 for _,input:=range []string{
  "{\"id\":\"0193fd8c-2075-7000-8000-000000000001\",\"state\":\"other\"}",
  "{\"id\":\"0193fd8c-2075-7000-8000-000000000001\",\"state\":\"active\",\"counts\":{\"a\":65536}}",
  "{\"id\":\"0193fd8c-2075-7000-8000-000000000001\",\"state\":\"active\",\"flags\":[true]}",
 } {
  _,err:=descriptor.Decode(context.Background(),[]byte(input),limits)
  var failure *contract.DecodeError
  if !errors.As(err,&failure){t.Fatalf("invalid DTO was accepted: %v",err)}
 }
 for _,field:=range []string{"\"sequence\":null","\"children\":null","\"bytes\":null"} {
  _,err:=descriptor.Decode(context.Background(),[]byte("{\"id\":\"0193fd8c-2075-7000-8000-000000000001\",\"state\":\"active\","+field+"}"),limits)
  if err!=nil{t.Fatalf("declared nullable value rejected: %v",err)}
 }
 description,err:=descriptor.Description();if err!=nil{t.Fatal(err)}
 if _,err:=json.Marshal(description);err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated public DTO failed: %v\n%s", err, output)
	}
}

func TestInvalidDTODeclarationsDoNotPublish(t *testing.T) {
	for name, source := range map[string]string{
		"nonstruct":                   "//foundry:dto\ntype Input string",
		"option":                      "//foundry:dto guessed=true\ntype Input struct{}",
		"alias":                       "type Original struct{}\n//foundry:dto\ntype Input = Original",
		"persistence":                 "//foundry:dto\ntype Input struct{ Name string `foundry:\"column=name\"` }",
		"func":                        "//foundry:dto\ntype Input struct{ Callback func() }",
		"channel":                     "//foundry:dto\ntype Input struct{ Channel chan int }",
		"nonempty interface":          "//foundry:dto\ntype Input struct{ Value interface{ Read() } }",
		"ambiguous":                   "type Left struct{ Name string };type Right struct{ Name string }\n//foundry:dto\ntype Input struct{ Left;Right }",
		"private embedded pointer":    "type hidden struct{ Name string }\n//foundry:dto\ntype Input struct{ *hidden }",
		"unknown json option":         "//foundry:dto\ntype Input struct{ Name string `json:\"name,guess\"` }",
		"custom codec":                "type Code string\nfunc(Code)MarshalJSON()([]byte,error){return nil,nil}\n//foundry:dto\ntype Input struct{ Code Code }",
		"quoted enum":                 "//foundry:enum\ntype State string\nconst Active State=\"active\"\n//foundry:dto\ntype Input struct{ State State `json:\"state,string\"` }",
		"nested model":                "//foundry:model table=accounts primary=Key\ntype Account struct{ Key int; Password string }\n//foundry:dto\ntype Input struct{ Account Account }",
		"embedded model":              "//foundry:model table=accounts primary=Key\ntype Account struct{ Key int; Password string }\n//foundry:dto\ntype Input struct{ Account }",
		"transitively embedded model": "//foundry:model table=accounts primary=Key\ntype Account struct{ Key int; Password string }\ntype Wrapper struct{ *Account }\n//foundry:dto\ntype Input struct{ Wrapper }",
		"symbol collision":            "//foundry:dto\ntype Input struct{}\nfunc InputJSON(){}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid DTO generated")
			} else if strings.Contains(name, "model") && !strings.Contains(err.Error(), "persistence models") {
				t.Fatalf("model fixture failed for an unrelated reason: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid DTO published output")
			}
		})
	}
}

func TestDTOChangesProduceStaleOutput(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:dto\ntype Input struct{ Name string `json:\"name\"` }")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", "package sample\n//foundry:dto\ntype Input struct{ Name string `json:\"display_name\"` }")
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("DTO tag edit was not stale: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("stale check changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generatedSnapshot(t, dir)["input_foundry.gen.go"], "display_name") {
		t.Fatal("DTO tag edit was not emitted")
	}
}

func TestDTOImportedEnumsAndModelsOnFreshCheckout(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/generator/domain"
//foundry:dto
type Input struct{ State domain.State }
`)
	if err := os.Mkdir(filepath.Join(dir, "domain"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "domain/types.go", `package domain
//foundry:enum
type State uint8
const Active State=1
//foundry:model table=accounts primary=Key
type Account struct{ Key int; Password string }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generatedSnapshot(t, dir)["input_foundry.gen.go"], "domain.State(0).EnumDescriptor()") {
		t.Fatal("imported enum descriptor was not reused")
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", `package sample
import "foundry.test/generator/domain"
//foundry:dto
type Input struct{ Account domain.Account }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err == nil || !strings.Contains(err.Error(), "persistence models") {
		t.Fatalf("imported model leaked into DTO: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("invalid imported model changed existing output")
	}
}
