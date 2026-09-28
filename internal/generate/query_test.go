package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const querySource = `package sample
import (
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/temporal"
)
type Member struct{}
type query uint16
type Flag bool
//foundry:enum
type State string
const Active State = "active"
type States []State
type MemberID = model.ID[Member]
//foundry:query
type SearchInput struct {
 Member MemberID ` + "`query:\"user\"`" + `
 Term value.Optional[string] ` + "`query:\"q\"`" + `
 Page value.Optional[query]
 Active value.Optional[Flag]
 States States ` + "`query:\"state\"`" + `
 Date value.Optional[temporal.Date]
 Amount value.Optional[decimal.Decimal]
 Ignored map[string]any ` + "`query:\"-\"`" + `
}
//foundry:query
type EmptyInput struct{}
var SearchParameters = SearchInputDescriptor()
func Parameters() foundryhttp.Query[SearchInput] { return SearchInputDescriptor() }
`

func TestFreshQueryGenerationAndRuntime(t *testing.T) {
	dir := fixture(t, querySource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh query stale check: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("check published output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["search_input_foundry.gen.go"]
	for _, want := range []string{
		"func SearchInputDescriptor()", "Query[SearchInput]", "ModelIDQuery[Member]", "StringQuery[string]",
		"IntegerQuery[query]", "BoolQuery[Flag]", "EnumQuery[State, *State]", "TextQuery[temporal.Date, *temporal.Date]",
		"TextQuery[decimal.Decimal, *decimal.Decimal]", "OptionalQueryParam[SearchInput, string]", "RepeatedQueryParam[SearchInput, State, States]", "query1 *SearchInput",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("query output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"Ignored", "Draft", dir} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("query output acquired %q", unwanted)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("query output is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "query_runtime_test.go", `package sample_test
import (
 "context"
 "errors"
 "testing"
 sample "foundry.test/generator"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)
func TestPublicQuery(t *testing.T) {
 limits:=foundryhttp.QueryLimits{Bytes:4096,Pairs:64,Issues:8}
 descriptor:=sample.SearchInputDescriptor()
 metadata,metadataErr:=descriptor.Parameters();if metadataErr!=nil{t.Fatal(metadataErr)}
 seenEnum:=false
 for _,parameter:=range metadata {
  if parameter.Scalar==nil{t.Fatal("fresh query scalar missing",parameter.Name)}
  if parameter.Name=="state" {seenEnum=true;if !parameter.Repeated || parameter.Scalar.Syntax!=foundryhttp.EnumURLSyntax || len(parameter.Scalar.Value.Cases)!=1{t.Fatal("fresh repeated enum metadata missing")}}
 }
 if !seenEnum{t.Fatal("fresh enum field missing")}
 raw:="user=0193fd8c-2075-7000-8000-000000000001&q=Jane+Doe%2B&page=0&active=false&state=active&state=active&date=2024-02-29&amount=12345678901234567890.001"
 input,err:=descriptor.Decode(context.Background(),raw,limits);if err!=nil{t.Fatal(err)}
 text,present:=input.Term.Get();if !present || text!="Jane Doe+"{t.Fatal("query text lost")}
 page,present:=input.Page.Get();if !present || page!=0{t.Fatal("present zero lost")}
 active,present:=input.Active.Get();if !present || bool(active){t.Fatal("present false lost")}
 amount,present:=input.Amount.Get();if !present || amount.String()!="12345678901234567890.001"{t.Fatal("decimal lost precision")}
 date,present:=input.Date.Get();if !present || date.String()!="2024-02-29"{t.Fatal("date changed")}
 if len(input.States)!=2 || input.States[0]!=sample.Active || input.States[1]!=sample.Active{t.Fatal("typed list changed")}
 encoded,err:=descriptor.Encode(context.Background(),input,limits);if err!=nil{t.Fatal(err)}
 again,err:=descriptor.Decode(context.Background(),encoded,limits);if err!=nil || again.Member!=input.Member{t.Fatal("query round trip failed")}
 input.States=sample.States{sample.State("undeclared")};if _,err:=descriptor.Encode(context.Background(),input,limits);err==nil{t.Fatal("enum output bypassed membership")}
 _,err=descriptor.Decode(context.Background(),"state=undeclared&user="+input.Member.String(),limits)
 var failure *foundryhttp.QueryError
 if !errors.As(err,&failure) || failure.Issues()[0].Path!="/state/0"{t.Fatalf("enum input issue: %v",err)}
 empty:=sample.EmptyInputDescriptor();if _,err:=empty.Decode(context.Background(),"",limits);err!=nil{t.Fatal(err)}
 if _,err:=empty.Decode(context.Background(),"unknown=value",limits);err==nil{t.Fatal("empty query accepted undeclared field")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated query consumer failed: %v\n%s", err, output)
	}
}

func TestInvalidQueryDeclarationsDoNotPublish(t *testing.T) {
	for name, source := range map[string]string{
		"options":            "//foundry:query source=query\ntype Input struct{}",
		"nonstruct":          "//foundry:query\ntype Input string",
		"unexported":         "//foundry:query\ntype Input struct{ field string }",
		"embedded":           "type Field struct{}\n//foundry:query\ntype Input struct{ Field }",
		"pointer":            "//foundry:query\ntype Input struct{ Field *string }",
		"pointer-optional":   "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field *value.Optional[string] }",
		"optional-pointer":   "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field value.Optional[*string] }",
		"nullable":           "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field value.Nullable[string] }",
		"optional-nullable":  "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field value.Optional[value.Nullable[string]] }",
		"optional-list":      "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field value.Optional[[]string] }",
		"nested-optional":    "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field value.Optional[value.Optional[string]] }",
		"list-nullable":      "import \"github.com/weiloon1234/Foundry-Go/value\"\n//foundry:query\ntype Input struct{ Field []value.Nullable[string] }",
		"interface":          "//foundry:query\ntype Input struct{ Field any }",
		"map":                "//foundry:query\ntype Input struct{ Field map[string]string }",
		"nested-list":        "//foundry:query\ntype Input struct{ Field [][]string }",
		"empty-name":         "//foundry:query\ntype Input struct{ Field string `query:\"\"` }",
		"bad-name":           "//foundry:query\ntype Input struct{ Field string `query:\"q&x\"` }",
		"tag-options":        "//foundry:query\ntype Input struct{ Field string `query:\"q,omitempty\"` }",
		"duplicate-name":     "//foundry:query\ntype Input struct{ First string `query:\"q\"`; Second string `query:\"q\"` }",
		"duplicate-tag":      "//foundry:query\ntype Input struct{ Field string `query:\"q\" query:\"x\"` }",
		"persistence-tag":    "//foundry:query\ntype Input struct{ Field string `foundry:\"column=q\"` }",
		"path-tag":           "//foundry:query\ntype Input struct{ Field string `path:\"q\"` }",
		"partial-codec":      "type Token string\nfunc(Token)MarshalText()([]byte,error){return nil,nil}\n//foundry:query\ntype Input struct{ Field Token }",
		"wrong-codec":        "type Token string\nfunc(Token)MarshalText()string{return \"\"}\n//foundry:query\ntype Input struct{ Field Token }",
		"partial-list-codec": "type Tokens []string\nfunc(Tokens)MarshalText()([]byte,error){return nil,nil}\n//foundry:query\ntype Input struct{ Field Tokens }",
		"symbol":             "//foundry:query\ntype Input struct{}\nfunc InputDescriptor(){}",
		"generic":            "//foundry:query\ntype Input[T any] struct{ Field T }",
		"alias":              "//foundry:query\ntype Input = struct{ Field string }",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid query generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid query published output")
			}
		})
	}
}

func TestQueryFieldChangesProduceStaleOutput(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:query\ntype Input struct{ Field string }")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", "package sample\n//foundry:query\ntype Input struct{ Renamed []string `query:\"filter[field]\"` }")
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed query was not stale: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("stale check changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["input_foundry.gen.go"]
	if !strings.Contains(output, "RepeatedQueryParam") || !strings.Contains(output, ".Renamed") || !strings.Contains(output, "filter[field]") {
		t.Fatal("query changes did not reach generated bindings")
	}
}

func TestQueryCustomTextCodecTakesPrecedence(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type Token[S ~string] struct{ Value S }
func(v Token[S])MarshalText()([]byte,error){_ = InputDescriptor;return []byte(v.Value),nil}
func(v *Token[S])UnmarshalText(data []byte)error{v.Value=S(data);return nil}
type Alias = Token[string]
type Packed []string
func(v Packed)MarshalText()([]byte,error){return nil,nil}
func(v *Packed)UnmarshalText([]byte)error{return nil}
//foundry:query
type Input struct { Token Alias; Packed Packed; Optional value.Optional[Packed]; Tokens []Alias }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["input_foundry.gen.go"]
	for _, want := range []string{"TextQuery[Alias, *Alias]", "QueryParam[Input, Packed]", "OptionalQueryParam[Input, Packed]", "RepeatedQueryParam[Input, Alias, []Alias]"} {
		if !strings.Contains(output, want) {
			t.Fatalf("custom text contract was not retained: %s", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
