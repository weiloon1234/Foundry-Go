package generate

import (
	"os/exec"
	"reflect"
	"testing"
)

// Use go run: a package loaded by go test does not have an executable's runtime
// main identity. All three generated boundaries must describe the same source.
func TestExecutableURLAndDTOShareSourceIdentities(t *testing.T) {
	dir := fixture(t, `package main
import(
 "context"
 "fmt"
 "github.com/weiloon1234/Foundry-Go/contract"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
 "github.com/weiloon1234/Foundry-Go/model"
)
type Member struct{}
type Code string
type Rank uint16
//foundry:path pattern=/{code}/{rank}/{id}
type LookupPath struct{Code Code;Rank Rank;ID model.ID[Member]}
//foundry:query
type LookupQuery struct{Code Code;Rank Rank;ID model.ID[Member]}
//foundry:dto
type LookupResponse struct{Code Code;Rank Rank;ID model.ID[Member];ByCode map[Code]string}
func main(){
 schema,err:=LookupResponseJSON().Description();if err!=nil{panic(err)}
 expected:=map[string]contract.TypeID{}
 for _,typ:=range schema.Types{
  if typ.ID==schema.Root{for _,field:=range typ.Properties{expected[field.Name]=field.Type}}
 }
 path,err:=LookupPathDescriptor().Parameters();if err!=nil{panic(err)}
 query,err:=LookupQueryDescriptor().Parameters();if err!=nil{panic(err)}
 names:=map[string]string{"code":"Code","rank":"Rank","id":"ID"}
 check:=func(name string,scalar *foundryhttp.URLScalarInfo){
  if scalar==nil || scalar.Value.ID!=expected[names[name]]{
   panic(fmt.Sprintf("source identity mismatch for %s: %+v; expected %s",name,scalar,expected[names[name]]))
  }
 }
 for _,parameter:=range path{check(parameter.Name,parameter.Scalar)}
 for _,parameter:=range query{check(parameter.Name,parameter.Scalar)}
 for _,typ:=range schema.Types{
  if typ.Key!=nil && typ.Key.Value.ID!=expected["Code"]{panic("map key source identity mismatch")}
 }
 ctx:=context.Background()
 input,err:=LookupQueryDescriptor().Decode(ctx,"code=example&rank=42&id=0193fd8c-2075-7000-8000-000000000001",foundryhttp.QueryLimits{Bytes:512,Pairs:10,Issues:10})
 if err!=nil || input.Code!="example" || input.Rank!=42{panic("query value contract changed")}
 if _,err:=LookupPathDescriptor().URL(LookupPath{Code:input.Code,Rank:input.Rank,ID:input.ID});err!=nil{panic(err)}
 if _,err:=LookupResponseJSON().Encode(ctx,LookupResponse{Code:input.Code,Rank:input.Rank,ID:input.ID,ByCode:map[Code]string{"example":"value"}},contract.JSONLimits{Bytes:1024,Depth:8,Nodes:30,Steps:100,Issues:10});err!=nil{panic(err)}
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("source identity generation changed")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("executable transport identity: %v\n%s", err, output)
	}
}
