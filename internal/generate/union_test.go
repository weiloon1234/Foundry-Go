package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const unionSource = `package sample
//foundry:dto
type Card struct{Token string ` + "`json:\"token\"`" + `}
//foundry:dto
type Bank struct{Reference string ` + "`json:\"reference\"`" + `}
//foundry:dto
type Box[T any] struct{Data T ` + "`json:\"data\"`" + `}
//foundry:union name=Payment discriminator=kind
type PaymentVariants struct{
 Card Card ` + "`union:\"card\"`" + `
 Bank Bank ` + "`union:\"bank\"`" + `
 Box Box[Card] ` + "`union:\"box\"`" + `
}
//foundry:dto
type Request struct{Method Payment ` + "`json:\"method\"`" + `;Next *Request ` + "`json:\"next,omitempty\"`" + `}
//foundry:union name=Recursive discriminator=kind
type RecursiveVariants struct{Node Node ` + "`union:\"node\"`" + `}
//foundry:dto
type Node struct{Name string ` + "`json:\"name\"`" + `;Next *Recursive ` + "`json:\"next,omitempty\"`" + `}
`

func TestUnionFreshGenerationRecursionAndRecovery(t *testing.T) {
	dir := fixture(t, unionSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	initial := generatedSnapshot(t, dir)
	for _, want := range []string{"type Payment struct", "PaymentFromCard", "func MatchPayment[R any]", "UnionValue[Payment]"} {
		if !strings.Contains(initial["payment_foundry.gen.go"], want) {
			t.Fatal("missing", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(initial, generatedSnapshot(t, dir)) {
		t.Fatal("union generation is not deterministic")
	}
	write(t, dir, "runtime_test.go", unionRuntime)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("union runtime: %v\n%s", err, output)
	}
	// Imported generated unions retain their own JSONContract and qualified ID.
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, child, "child.go", `package child
import sample "foundry.test/generator"
//foundry:dto
type Imported struct{Method sample.Payment}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	write(t, child, "child_test.go", `package child
import("testing";"github.com/weiloon1234/Foundry-Go/contract")
func TestImported(t *testing.T){
 limits:=contract.JSONLimits{Bytes:4096,Depth:8,Nodes:100,Steps:500,Issues:8}
 v,err:=ImportedJSON().Decode(t.Context(),[]byte("{\"Method\":{\"kind\":\"card\",\"token\":\"test\"}}"),limits)
 if err!=nil{t.Fatal(err)};if _,ok:=v.Method.Card();!ok{t.Fatal("missing imported card")}
}
`)
	command = exec.CommandContext(t.Context(), "go", "test", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("imported union: %v\n%s", err, output)
	}
}

const unionRuntime = `package sample
import("encoding/json";"testing";"github.com/weiloon1234/Foundry-Go/contract")
func TestRoundtrip(t *testing.T){
 p,err:=PaymentFromCard(Card{Token:"test"});if err!=nil{t.Fatal(err)}
 raw,err:=json.Marshal(p);if err!=nil{t.Fatal(err)}
 var next Payment;if err=json.Unmarshal(raw,&next);err!=nil{t.Fatal(err)}
 card,ok:=next.Card();if !ok||card.Token!="test"{t.Fatal("accessor")}
 s,err:=RequestJSON().Description();if err!=nil{t.Fatal(err)}
 if _,err=s.Normalize();err!=nil{t.Fatal(err)}
 limits:=contract.JSONLimits{Bytes:8192,Depth:8,Nodes:100,Steps:500,Issues:8}
 r,err:=RecursiveJSON().Decode(t.Context(),[]byte("{\"kind\":\"node\",\"name\":\"one\",\"next\":{\"kind\":\"node\",\"name\":\"two\"}}"),limits)
 if err!=nil{t.Fatal(err)}
 node,ok:=r.Node();if !ok||node.Next==nil{t.Fatal("recursion lost")}
 if _,err=RecursiveJSON().Encode(t.Context(),r,limits);err!=nil{t.Fatal(err)}
}
`

func TestUnionRejectsInvalidDeclarationsAndCalls(t *testing.T) {
	cases := map[string]string{
		"duplicate_tag":   strings.Replace(unionSource, `union:"bank"`, `union:"card"`, 1),
		"field_collision": strings.Replace(unionSource, `json:"token"`, `json:"kind"`, 1),
		"empty": `package sample
//foundry:union name=Empty discriminator=kind
type EmptyVariants struct{}`,
		"scalar":      strings.Replace(unionSource, "Card Card `", "Card string `", 1),
		"pointer":     strings.Replace(unionSource, "Card Card `", "Card *Card `", 1),
		"symbol":      unionSource + "\nvar PaymentFromCard=7\n",
		"constructor": unionSource + "\nvar _,_=PaymentFromCard(Bank{})\n",
		"visitor":     unionSource + "\nvar _,_=MatchPayment(Payment{},func(Card)(string,error){return \"\",nil})\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid union generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("partial output published")
			}
		})
	}
}

func TestUnionExecutableIdentity(t *testing.T) {
	source := strings.Replace(unionSource, "package sample", "package main", 1) + `
func main(){p,err:=PaymentFromCard(Card{Token:"test"});if err!=nil{panic(err)};if _,ok:=p.Card();!ok{panic("missing")};if err=PaymentJSON().Validate();err!=nil{panic(err)}}`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("main union: %v\n%s", err, output)
	}
}
