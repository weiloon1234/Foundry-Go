package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const streamingDTOFixture = `package sample
import(
 "encoding/json/jsontext"
 jsonv2 "encoding/json/v2"
 "github.com/weiloon1234/Foundry-Go/contract"
)
type StreamCode struct{ text string }
func(v StreamCode)MarshalJSONTo(enc *jsontext.Encoder)error{return enc.WriteToken(jsontext.String(v.text))}
func(v *StreamCode)UnmarshalJSONFrom(dec *jsontext.Decoder)error{
 var text string;if err:=jsonv2.UnmarshalDecode(dec,&text);err!=nil{return err};v.text=text;return nil
}
func(StreamCode)JSONContract()contract.JSON[StreamCode]{
 return contract.ScalarJSON(contract.DefineScalar[StreamCode](contract.Type{ID:"foundry.test/generator.StreamCode",Kind:contract.StringKind}))
}
//foundry:dto
type Response struct{ Code StreamCode; History []StreamCode }
`

func TestFreshStreamingDTOGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, streamingDTOFixture)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if !strings.Contains(first["response_foundry.gen.go"], "JSONType[StreamCode]") {
		t.Fatal("streaming contract not discovered")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("streaming generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "stream_runtime_test.go", `package sample_test
import(
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestGeneratedNativeStreamingContract(t *testing.T){
 d:=sample.ResponseJSON()
 limits:=contract.JSONLimits{Bytes:1024,Depth:8,Nodes:100,Steps:200,Issues:10}
 input,err:=d.Decode(t.Context(),[]byte("{\"Code\":\"first\",\"History\":[\"second\"]}"),limits)
 if err!=nil{t.Fatal(err)}
 if _,err:=d.Encode(t.Context(),input,limits);err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("streaming generated consumer: %v\n%s", err, output)
	}
}

func TestInvalidStreamingDTOCodecsDoNotPublish(t *testing.T) {
	for name, methods := range map[string]string{
		"wrong_decoder":   "func(StreamCode)MarshalJSONTo(enc *jsontext.Encoder)error{return enc.WriteToken(jsontext.String(\"x\"))}\nfunc(*StreamCode)UnmarshalJSONFrom(*jsontext.Encoder)error{return nil}",
		"pointer_encoder": "func(*StreamCode)MarshalJSONTo(enc *jsontext.Encoder)error{return enc.WriteToken(jsontext.String(\"x\"))}\nfunc(*StreamCode)UnmarshalJSONFrom(*jsontext.Decoder)error{return nil}",
	} {
		t.Run(name, func(t *testing.T) {
			source := "package sample\nimport(\"encoding/json/jsontext\"; \"github.com/weiloon1234/Foundry-Go/contract\")\ntype StreamCode string\n" + methods + "\nfunc(StreamCode)JSONContract()contract.JSON[StreamCode]{return contract.JSON[StreamCode]{}}\n//foundry:dto\ntype Response struct{Code StreamCode}\n"
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "JSONContract requires") {
				t.Fatal("invalid streaming codec accepted/wrong diagnostic", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid streaming codec published output")
			}
		})
	}
}
