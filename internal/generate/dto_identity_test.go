package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestNamedGenericCustomContractsShareGeneratedAndRuntimeIdentity(t *testing.T) {
	dir := fixture(t, `package sample
import("encoding/json";"reflect";"github.com/weiloon1234/Foundry-Go/contract";"github.com/weiloon1234/Foundry-Go/auth/challenge";"github.com/weiloon1234/Foundry-Go/auth/passwordreset")
type Member struct{}
type Admin struct{}
type Wrapper[T any] struct{}
type Pair[A,B any] struct{value string}
func(p Pair[A,B])MarshalJSON()([]byte,error){return json.Marshal(p.value)}
func(p *Pair[A,B])UnmarshalJSON(data []byte)error{return json.Unmarshal(data,&p.value)}
func(Pair[A,B])JSONContract()contract.JSON[Pair[A,B]]{t:=reflect.TypeFor[Pair[A,B]]();id:=contract.TypeID(t.PkgPath()+"."+t.Name());return contract.DefineJSONValue[Pair[A,B]](contract.Schema{Root:id,Types:[]contract.Type{{ID:id,Kind:contract.StringKind}}})}
//foundry:dto
type Request struct {
 Reset passwordreset.Token[Member]
 Verify challenge.Token[Member,challenge.EmailVerification]
 Admin challenge.Token[Admin,challenge.PasswordReset]
 Nested Pair[Wrapper[Member],Wrapper[Admin]]
 Scalars Pair[byte,rune]
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	snapshot := generatedSnapshot(t, dir)
	want := "github.com/weiloon1234/Foundry-Go/auth/challenge.Token[foundry.test/generator.Member,github.com/weiloon1234/Foundry-Go/auth/challenge.PasswordReset]"
	if !strings.Contains(snapshot["request_foundry.gen.go"], want) {
		t.Fatal("typed token contract identity was hashed or spaced")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, generatedSnapshot(t, dir)) {
		t.Fatal("generic contract generation changed")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "generic_runtime_test.go", `package sample_test
import("testing";"strings";sample "foundry.test/generator";"github.com/weiloon1234/Foundry-Go/contract")
func TestGenericInput(t *testing.T){
 d:=sample.RequestJSON();if err:=d.Validate();err!=nil{t.Fatal(err)}
 raw:="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
 body:=[]byte("{\"Reset\":\""+raw+"\",\"Verify\":\""+raw+"\",\"Admin\":\""+raw+"\",\"Nested\":\"n\",\"Scalars\":\"s\"}")
 limits:=contract.JSONLimits{Bytes:2048,Depth:8,Nodes:100,Steps:200,Issues:8}
 value,err:=d.Decode(t.Context(),body,limits);if err!=nil{t.Fatal(err)}
 if value.Reset.Secret().Reveal()!=raw||value.Verify.Secret().Reveal()!=raw{t.Fatal("typed input lost")}
 encoded,err:=d.Encode(t.Context(),value,limits);if err!=nil||strings.Contains(string(encoded),raw){t.Fatal("token was not redacted",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generic consumer: %v\n%s", err, output)
	}
}

func TestExecutableGenericRecoveryContractsRetainSourceIdentity(t *testing.T) {
	dir := fixture(t, `package main
import("context";"github.com/weiloon1234/Foundry-Go/auth/challenge";"github.com/weiloon1234/Foundry-Go/auth/mfa";"github.com/weiloon1234/Foundry-Go/contract")
type Member struct{}
//foundry:dto
type Request struct{Token challenge.Token[Member,challenge.PasswordReset];Enrollment mfa.EnrollmentID[Member]}
func main(){
 d:=RequestJSON();if err:=d.Validate();err!=nil{panic(err)}
 description,err:=d.Description();if err!=nil{panic(err)}
 expected:=contract.TypeID("github.com/weiloon1234/Foundry-Go/auth/challenge.Token[foundry.test/generator.Member,github.com/weiloon1234/Foundry-Go/auth/challenge.PasswordReset]")
 found:=false
 for _,typ:=range description.Types{if typ.ID==description.Root{for _,p:=range typ.Properties{if p.Name=="Token"{found=p.Type==expected}}}}
 if !found{panic("main argument lost generated source identity")}
 limits:=contract.JSONLimits{Bytes:1024,Depth:8,Nodes:100,Steps:200,Issues:8}
 body:=[]byte("{\"Token\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\",\"Enrollment\":\"0193fd8c-2075-7000-8000-000000000001\"}")
 value,err:=d.Decode(context.Background(),body,limits);if err!=nil{panic(err)}
 if value.Enrollment.IsZero()||value.Token.Secret().IsZero(){panic("decoded empty values")}
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("executable generic contracts: %v\n%s", err, output)
	}
}
