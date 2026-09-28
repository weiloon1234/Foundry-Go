package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestModelReferenceGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/decimal";"github.com/weiloon1234/Foundry-Go/value")
type NaturalKey string
func(NaturalKey)MarshalJSON()([]byte,error){panic("presentation key must not run")}
//foundry:model table=members primary=Key
type Member struct{Key NaturalKey;Password string}
func(Member)AccessKey()(string,error){panic("getter must not run")}
//foundry:model table=teams primary=ID
type Team struct{ID model.ID[Team]}
//foundry:model table=exact_keys primary=Key
type ExactKey struct{Key decimal.Decimal}
type CompositeKey struct{Tenant int;Name string}
//foundry:model table=json_keys primary=Key
type JSONKey struct{Key value.JSON[CompositeKey]}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	found := false
	for _, data := range first {
		if strings.Contains(data, "FoundryReference") && strings.Contains(data, "AccessKey") {
			found = true
		}
	}
	if !found {
		t.Fatal("generated reference lacks stored-key getter notice")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("model reference generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "reference_test.go", `package sample
import("encoding/json";"strings";"testing";"github.com/weiloon1234/Foundry-Go/attribution";"github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/decimal";"github.com/weiloon1234/Foundry-Go/value")
func TestStoredReferences(t *testing.T){
 member:=Member{Key:" stored ",Password:"never-export"}
 var reference model.Reference[Member,NaturalKey]=member.FoundryReference()
 identity,err:=member.FoundryIdentity();if err!=nil{t.Fatal(err)}
 restored,err:=(Member{}).FoundryReference().Parse(identity);if err!=nil||restored.Key()!=reference.Key(){t.Fatal("stored natural key changed",err)}
 origin,err:=(attribution.Origin{}).WithModel(member);if err!=nil{t.Fatal(err)}
 encoded,err:=json.Marshal(origin);if err!=nil||strings.Contains(string(encoded),member.Password){t.Fatal("origin exposed the model",err)}
 id,err:=model.NewID[Team]();if err!=nil{t.Fatal(err)}
 team:=Team{ID:id};teamIdentity,err:=team.FoundryIdentity();if err!=nil{t.Fatal(err)}
 teamReference,err:=(Team{}).FoundryReference().Parse(teamIdentity);if err!=nil||teamReference.Key()!=id{t.Fatal("model-owned UUID changed",err)}
 exact,err:=decimal.Parse("123456789012345678901234567890.12345");if err!=nil{t.Fatal(err)}
 exactIdentity,err:=(ExactKey{Key:exact}).FoundryIdentity();if err!=nil{t.Fatal(err)}
 exactReference,err:=(ExactKey{}).FoundryReference().Parse(exactIdentity);if err!=nil||exactReference.Key()!=exact{t.Fatal("exact key changed",err)}
 composite,err:=value.NewJSON(CompositeKey{Tenant:7,Name:"stored"});if err!=nil{t.Fatal(err)}
 compositeIdentity,err:=(JSONKey{Key:composite}).FoundryIdentity();if err!=nil{t.Fatal(err)}
 compositeReference,err:=(JSONKey{}).FoundryReference().Parse(compositeIdentity);if err!=nil||compositeReference.Key()!=composite{t.Fatal("JSON SQL key changed",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated model references: %v\n%s", err, output)
	}
}

func TestModelReferenceRejectsMemberCollisionsBeforePublication(t *testing.T) {
	for _, declaration := range []string{
		"type User struct{ID int;FoundryReference string}",
		"type User struct{ID int;FoundryIdentity string}",
		"type User struct{ID int}\nfunc(User)FoundryReference()int{return 0}",
		"type User struct{ID int}\nfunc(User)FoundryIdentity()int{return 0}",
	} {
		dir := fixture(t, "package sample\n//foundry:model table=users primary=ID\n"+declaration)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("conflicting generated reference member accepted")
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid declaration published generated files")
		}
	}
}
