package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const genericDTOSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/validation"
)
//foundry:dto
type Leaf struct{ Name string }
type Items[T any] []T
//foundry:dto
type Envelope[T any] struct {
 Data T ` + "`json:\"data\"`" + `
 Items []T ` + "`json:\"items,omitempty\"`" + `
 Named Items[T] ` + "`json:\"named,omitempty\"`" + `
 Index map[string]T ` + "`json:\"index,omitempty\"`" + `
 Direct value.Optional[T] ` + "`json:\"direct,omitzero\"`" + `
 Maybe value.Optional[value.Nullable[T]] ` + "`json:\"maybe,omitzero\"`" + `
 Next *Envelope[T] ` + "`json:\"next,omitempty\"`" + `
}
//foundry:dto
type Pair[A,B any] struct{ First A; Second B }
//foundry:dto
type Tagged[T ~string] struct{ Value T }
//foundry:dto
type Lookup[K comparable,V any] struct{ Entries map[K]V }
//foundry:dto
type Phantom[T any] struct{ Count int }
//foundry:dto
type Collision[foundrycontract any,foundryType0 any,input any] struct{ Data foundrycontract; Value foundryType0; Other input }
//foundry:dto
type Nested struct{Value Envelope[Leaf];Bytes Envelope[byte];Pair Pair[Leaf,string];Collection Envelope[[]byte]}
type Wrapped = Envelope[Leaf]
var wire = EnvelopeJSON(LeafJSON())
var byteWire = EnvelopeJSON(contract.IntegerJSON[byte]())
var nestedWire = EnvelopeJSON(EnvelopeJSON(LeafJSON()))
var pairWire = PairJSON(LeafJSON(),contract.StringJSON[string]())
var lookupWire = LookupJSON(contract.StringJSON[string](),contract.StringJSONKey[string](),LeafJSON())
func Rules() validation.Rule[Envelope[int]] {return EnvelopeValidationFields[int]().Data.Rules(validation.Min[int](1))}
`

func TestGenericDTOGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, genericDTOSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"func EnvelopeJSON[T any]", "JSON[Envelope[T]]", "EnvelopeValidationFieldSet[T any]", "EnvelopeValidationFields[T any]", "DefineGenericJSON", "JSONSliceType"} {
		if !strings.Contains(first["envelope_foundry.gen.go"], want) {
			t.Fatalf("missing generic contract %q", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("generic output changed")
	}
	write(t, dir, "generic_runtime_test.go", genericDTORuntime)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generic DTO consumer: %v\n%s", err, output)
	}
}

const genericDTORuntime = `package sample_test
import (
 "context"
 "encoding/json"
 "reflect"
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/contract/manifest"
 "github.com/weiloon1234/Foundry-Go/openapi"
 "github.com/weiloon1234/Foundry-Go/typescript"
 "github.com/weiloon1234/Foundry-Go/validation"
 "github.com/weiloon1234/Foundry-Go/value"
)
var limits=contract.JSONLimits{Bytes:8192,Depth:16,Nodes:500,Steps:2000,Issues:16}
func round[T any](t *testing.T,d contract.JSON[T],input string)T{
 t.Helper();if err:=d.Validate();err!=nil{t.Fatal(err)}
 v,err:=d.Decode(t.Context(),[]byte(input),limits);if err!=nil{t.Fatalf("decode %s: %v",input,err)}
 encoded,err:=d.Encode(t.Context(),v,limits);if err!=nil{t.Fatal(err)}
 again,err:=d.Decode(t.Context(),encoded,limits);if err!=nil||!reflect.DeepEqual(v,again){t.Fatal("generic roundtrip",err)}
 return v
}
func TestGenericRoundTrips(t *testing.T){
 d:=sample.EnvelopeJSON(sample.LeafJSON())
 v:=round(t,d,"{\"data\":{\"Name\":\"one\"},\"maybe\":null,\"next\":{\"data\":{\"Name\":\"two\"}}}")
 if v.Data.Name!="one"||v.Next.Data.Name!="two"{t.Fatal("generic fields changed")}
 maybe,ok:=v.Maybe.Get();if !ok||!maybe.IsNull(){t.Fatal("presence lost")}
 bytes:=round(t,sample.EnvelopeJSON(contract.IntegerJSON[uint8]()),"{\"data\":7,\"items\":\"YWI=\",\"named\":\"YWI=\"}")
 if string(bytes.Named)!="ab"{t.Fatal("named byte slice lost base64 representation")}
 named:=round(t,d,"{\"data\":{\"Name\":\"one\"},\"named\":[{\"Name\":\"two\"}]}")
 if len(named.Named)!=1||named.Named[0].Name!="two"{t.Fatal("named nonbyte slice lost array representation")}
 round(t,sample.EnvelopeJSON(contract.IntegerJSON[byte]()),"{\"data\":0,\"named\":null}")
 round(t,sample.EnvelopeJSON(contract.Nullable(contract.StringJSON[string]())),"{\"data\":null,\"direct\":null}")
 round(t,sample.LookupJSON(contract.StringJSON[string](),contract.StringJSONKey[string](),sample.LeafJSON()),"{\"Entries\":{\"key\":{\"Name\":\"value\"}}}")
 round(t,sample.PairJSON(contract.StringJSON[string](),contract.StringJSON[string]()),"{\"First\":\"a\",\"Second\":\"b\"}")
 round(t,sample.EnvelopeJSON(sample.EnvelopeJSON(sample.LeafJSON())),"{\"data\":{\"data\":{\"Name\":\"nested\"}}}")
 if err:=sample.Rules().Check(t.Context(),sample.Envelope[int]{Data:0},validation.DefaultLimits());err==nil{t.Fatal("typed rule omitted")}
 _=sample.CollisionJSON(contract.StringJSON[string](),contract.IntegerJSON[int](),sample.LeafJSON())
 _=sample.CollisionValidationFields[string,int,sample.Leaf]()
}
func TestGenericAndConcreteMetadataAgree(t *testing.T){
 descriptors:=[]interface{Description()(contract.Schema,error)}{
  sample.NestedJSON(),sample.EnvelopeJSON(sample.LeafJSON()),sample.EnvelopeJSON(contract.IntegerJSON[uint8]()),
  sample.PairJSON(sample.LeafJSON(),contract.StringJSON[string]()),sample.EnvelopeJSON(contract.Slice(contract.IntegerJSON[byte]())),
 }
 sources:=manifest.Sources{}
 for _,d:=range descriptors{s,err:=d.Description();if err!=nil{t.Fatal(err)};sources.Schemas=append(sources.Schemas,s)}
 m,err:=manifest.Build(context.Background(),sources);if err!=nil{t.Fatal("concrete/generic identities diverged",err)}
 if _,err:=openapi.Render(m,openapi.Options{Title:"Generic",APIVersion:"1"});err!=nil{t.Fatal(err)}
 if _,err:=typescript.Render(m);err!=nil{t.Fatal(err)}
 wire,err:=m.JSON();if err!=nil||!json.Valid(wire){t.Fatal(err)}
}
func TestGenericInvalidArguments(t *testing.T){
 if sample.PhantomJSON(contract.JSON[int]{}).Validate()==nil{t.Fatal("invalid unused argument accepted")}
 strict:=contract.DefineJSONField[string](contract.Schema{Root:"string",Types:[]contract.Type{{ID:"string",Kind:contract.StringKind,Cases:[]json.RawMessage{json.RawMessage("\"one\"")}}}})
 if sample.PairJSON(strict,contract.StringJSON[string]()).Validate()==nil{t.Fatal("conflicting same-type arguments accepted")}
 if _,err:=sample.EnvelopeJSON(contract.StringJSON[string]()).Decode(t.Context(),[]byte("{\"data\":5}"),limits);err==nil{t.Fatal("wrong generic wire value")}
 var _ value.Optional[value.Nullable[sample.Leaf]]=sample.Envelope[sample.Leaf]{}.Maybe
}
`

func TestGenericDTOExecutableSourceIdentity(t *testing.T) {
	source := strings.Replace(genericDTOSource, "package sample", "package main", 1) + `
func main(){
 d:=EnvelopeJSON(LeafJSON())
 s,err:=d.Description();if err!=nil{panic(err)}
 if s.Root!="foundry.test/generator.Envelope[foundry.test/generator.Leaf]"{panic("main identity lost")}
 if err:=EnvelopeJSON(EnvelopeJSON(LeafJSON())).Validate();err!=nil{panic(err)}
}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generic main: %v\n%s", err, output)
	}
}

func TestGenericDTORejectsInvalidDeclarationsAndUsage(t *testing.T) {
	for name, extra := range map[string]string{
		"wrong_argument": `var invalid = EnvelopeJSON[int](contract.StringJSON[string]())`,
		"constraint":     `var invalid = TaggedJSON(contract.IntegerJSON[int]())`,
		"wrong_key":      `var invalid = LookupJSON(contract.StringJSON[string](),contract.IntegerJSONKey[int](),LeafJSON())`,
		"wrong_rule":     `var invalid = EnvelopeValidationFields[string]().Data.Rules(validation.Min[int](1))`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, genericDTOSource+"\n"+extra)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid generic usage generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid source published generated files")
			}
		})
	}
}
