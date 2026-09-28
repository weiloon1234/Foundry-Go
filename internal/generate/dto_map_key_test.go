package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const mapKeyDTOFixture = `package sample
import(
 "strconv"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/model"
)
type Member struct{}
//foundry:enum
type State string
const Ready State = "ready"
type CustomKey struct { n int }
func(k CustomKey) MarshalText()([]byte,error){return []byte(strconv.Itoa(k.n)),nil}
func(k *CustomKey) UnmarshalText(data []byte)error{
 n,err:=strconv.Atoi(string(data));k.n=n;return err
}
func(CustomKey) JSONKeyContract()contract.JSONKey[CustomKey]{
 return contract.DefineJSONKey(contract.DefineScalar[CustomKey](contract.Type{ID:"foundry.test/generator.CustomKey",Kind:contract.StringKind}))
}
//foundry:dto
type Result struct {
 Integers map[int8]string
 Enums map[State]string
 Members map[model.ID[Member]]string
 Custom map[CustomKey]string
}
`

func TestFreshMapKeyDTOGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, mapKeyDTOFixture)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"JSONMapType[map[int8]string]", "IntegerJSONKey[int8]", "EnumJSONKey", "ModelIDJSONKey[Member]", "ResolveJSONKey[CustomKey]", "(*new(CustomKey)).JSONKeyContract"} {
		if !strings.Contains(first["result_foundry.gen.go"], want) {
			t.Fatal("missing typed map output", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("map generation nondeterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "map_runtime_test.go", `package sample_test
import(
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestGeneratedMapKeys(t *testing.T){
 d:=sample.ResultJSON()
 limits:=contract.JSONLimits{Bytes:2048,Depth:16,Nodes:100,Steps:300,Issues:10}
 input,err:=d.Decode(t.Context(),[]byte("{\"Integers\":{\"-128\":\"low\"},\"Enums\":{\"ready\":\"yes\"},\"Members\":{},\"Custom\":{\"42\":\"stock\"}}"),limits)
 if err!=nil || input.Integers[-128]!="low" || input.Enums[sample.Ready]!="yes"{t.Fatal("decode",err)}
 if _,err:=d.Encode(t.Context(),input,limits);err!=nil{t.Fatal("encode",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated map consumer: %v\n%s", err, output)
	}
}

func TestInvalidDTOMapKeyDeclarationsDoNotPublish(t *testing.T) {
	for name, declaration := range map[string]string{
		"boolean":                 "type Key bool",
		"pointer":                 "type Key = *string",
		"custom_without_contract": "type Key string\nfunc(k *Key)UnmarshalText([]byte)error{return nil}",
		"pointer_factory":         "type Key string\nfunc(*Key)JSONKeyContract()contract.JSONKey[Key]{return contract.JSONKey[Key]{}}",
		"wrong_owner":             "type Key string\nfunc(Key)JSONKeyContract()contract.JSONKey[string]{return contract.StringJSONKey[string]()}",
		"arguments":               "type Key string\nfunc(Key)JSONKeyContract(string)contract.JSONKey[Key]{return contract.JSONKey[Key]{}}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\nimport \"github.com/weiloon1234/Foundry-Go/contract\"\nvar _ = contract.StringKind\n"+declaration+"\n//foundry:dto\ntype Result struct{Values map[Key]string}\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid map key accepted")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid map key published")
			}
		})
	}
}
