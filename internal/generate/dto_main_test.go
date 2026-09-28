package generate

import (
	"os/exec"
	"testing"
)

// Main packages are compiled with a special runtime package identity. Verify
// an actual executable, not only a package compiled as part of go test.
func TestDTOInExecutableMainPackage(t *testing.T) {
	dir := fixture(t, `package main
import (
 "context"
 "github.com/weiloon1234/Foundry-Go/contract"
)
//foundry:dto
type Input struct{ Name string }
func main(){
 descriptor:=InputJSON()
 if err:=descriptor.Validate();err!=nil{panic(err)}
 value,err:=descriptor.Decode(context.Background(),[]byte("{\"Name\":\"valid\"}"),contract.JSONLimits{Bytes:256,Depth:4,Nodes:20,Steps:100,Issues:10})
 if err!=nil{panic(err)}
 if value.Name!="valid"{panic("decoded main-package DTO changed")}
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated main-package DTO failed: %v\n%s", err, output)
	}
}
