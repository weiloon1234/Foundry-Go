package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const customDTOFixture = `package sample
import(
 "encoding/json"
 "strings"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Reference string
func(v Reference) MarshalText()([]byte,error){ return []byte(v),nil }
func(v *Reference) UnmarshalText(data []byte)error{*v=Reference(data);return nil}
func(Reference) JSONContract()contract.JSON[Reference]{
 return contract.DefineJSONValue[Reference](contract.Schema{Root:"foundry.test/generator.Reference",Types:[]contract.Type{
  {ID:"foundry.test/generator.Reference",Kind:contract.StringKind},
 }})
}
type Secret struct { stored string }
func(v Secret) MarshalJSON()([]byte,error){return json.Marshal("display:"+v.stored)}
func(v *Secret) UnmarshalJSON(data []byte)error{
 var text string
 if err:=json.Unmarshal(data,&text);err!=nil{return err}
 v.stored=strings.TrimPrefix(text,"display:")
 return nil
}
func(Secret) JSONContract()contract.JSON[Secret]{
 return contract.DefineJSONValue[Secret](contract.Schema{Root:"foundry.test/generator.Secret",Types:[]contract.Type{
  {ID:"foundry.test/generator.Secret",Kind:contract.StringKind},
 }})
}
//foundry:dto
type Result struct {
 Reference Reference
 Values []Reference
 Next value.Optional[value.Nullable[Reference]] ` + "`json:\"Next,omitzero\"`" + `
 Secret Secret
}
`

func TestFreshCustomDTOGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, customDTOFixture)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"JSONType[Reference]", "(*new(Reference)).JSONContract", "JSONType[Secret]", "(*new(Secret)).JSONContract"} {
		if !strings.Contains(first["result_foundry.gen.go"], want) {
			t.Fatal("custom generation missing", want)
		}
	}
	if strings.Contains(first["result_foundry.gen.go"], "stored") {
		t.Fatal("private codec representation inferred")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("nondeterministic custom generation")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "custom_runtime_test.go", `package sample_test
import(
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestGeneratedNativeCustomValues(t *testing.T){
 d:=sample.ResultJSON()
 limits:=contract.JSONLimits{Bytes:2048,Depth:16,Nodes:100,Steps:200,Issues:10}
 input,err:=d.Decode(t.Context(),[]byte("{\"Reference\":\"ref_1\",\"Values\":[\"ref_2\"],\"Secret\":\"display:private\"}"),limits)
 if err!=nil || input.Reference!=sample.Reference("ref_1"){t.Fatal("decode",err)}
 output,err:=d.Encode(t.Context(),input,limits)
 if err!=nil || len(output)==0{t.Fatal("encode",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("custom generated consumer: %v\n%s", err, output)
	}
}

func TestInvalidCustomDTOContractMethodsDoNotPublish(t *testing.T) {
	for name, method := range map[string]string{
		"wrong_value":     "func(Code)JSONContract()contract.JSON[Other]{return contract.JSON[Other]{}}",
		"pointer_factory": "func(*Code)JSONContract()contract.JSON[Code]{return contract.JSON[Code]{}}",
		"arguments":       "func(Code)JSONContract(string)contract.JSON[Code]{return contract.JSON[Code]{}}",
		"one_way":         "func(Code)JSONContract()contract.JSON[Code]{return contract.JSON[Code]{}}",
	} {
		t.Run(name, func(t *testing.T) {
			source := "package sample\nimport \"github.com/weiloon1234/Foundry-Go/contract\"\ntype Code string\ntype Other string\nfunc(Code)MarshalText()([]byte,error){return nil,nil}\n" + method + "\n//foundry:dto\ntype Result struct{Code Code}\n"
			if name != "one_way" {
				source += "func(*Code)UnmarshalText([]byte)error{return nil}\n"
			}
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "JSONContract") {
				t.Fatal("invalid custom factory accepted or wrong diagnostic", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid factory published")
			}
		})
	}
}

func TestPrivateCustomDTOContractIsRejectedBeforePublication(t *testing.T) {
	source := strings.ReplaceAll(customDTOFixture, "Reference", "reference")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "exported named types") {
		t.Fatal("private custom contract lacked a construction diagnostic", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("private custom contract published")
	}
}
