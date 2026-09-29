package generate

import (
	"os/exec"
	"strings"
	"testing"
)

const nonNullCollectionSource = `package sample
import "github.com/weiloon1234/Foundry-Go/value"
//foundry:dto
type Listing struct {
 Tags value.List[string] ` + "`json:\"tags\"`" + `
 Notes []string ` + "`json:\"notes\"`" + `
}
//foundry:dto
type Page[T any] struct {
 Items value.List[T] ` + "`json:\"items\"`" + `
}
var pageWire = PageJSON[string]
`

// value.List encodes nil as [] and rejects null, while ordinary slices keep
// Go's nullable default.
func TestNonNullListsEncodeEmptyAndRejectNull(t *testing.T) {
	dir := fixture(t, nonNullCollectionSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if output := generatedSnapshot(t, dir)["listing_foundry.gen.go"]; !strings.Contains(output, `ID: "github.com/weiloon1234/Foundry-Go/value.List[string]", Kind: foundrycontract.Kind("array"), Nullable: false`) {
		t.Fatalf("non-null list node missing:\n%s", output)
	}
	write(t, dir, "collections_runtime_test.go", `package sample_test
import (
 "context"
 "testing"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/contract"
)
func TestNonNullLists(t *testing.T) {
 limits:=contract.JSONLimits{Bytes:4096,Depth:16,Nodes:500,Steps:2000,Issues:20}
 data,err:=sample.ListingJSON().Encode(context.Background(),sample.Listing{},limits)
 if err!=nil||string(data)!="{\"tags\":[],\"notes\":null}"{t.Fatalf("encoded %s, %v",data,err)}
 if _,err:=sample.ListingJSON().Decode(context.Background(),[]byte("{\"tags\":null,\"notes\":[]}"),limits);err==nil{t.Fatal("null list accepted")}
 decoded,err:=sample.ListingJSON().Decode(context.Background(),[]byte("{\"tags\":[\"a\"],\"notes\":null}"),limits)
 if err!=nil||len(decoded.Tags)!=1||decoded.Tags[0]!="a"{t.Fatal(decoded,err)}
 page,err:=sample.PageJSON[string](contract.StringJSON[string]()).Encode(context.Background(),sample.Page[string]{},limits)
 if err!=nil||string(page)!="{\"items\":[]}"{t.Fatalf("generic page %s, %v",page,err)}
 schema,err:=sample.PageJSON[string](contract.StringJSON[string]()).Description()
 if err!=nil{t.Fatal(err)}
 found:=false
 for _,typ:=range schema.Types{found=found||typ.Kind==contract.ArrayKind&&!typ.Nullable}
 if !found{t.Fatal("generic page lost its non-null list")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("non-null lists failed: %v\n%s", err, output)
	}
}

func TestByteListIsRejected(t *testing.T) {
	dir := fixture(t, "package sample\nimport \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:dto\ntype Input struct{ Data value.List[byte] `json:\"data\"` }")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "value.List of bytes") {
		t.Fatal("byte list generated", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid declaration published output")
	}
}
