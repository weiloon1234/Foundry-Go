package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestRelationWriteDraftGeneration(t *testing.T) {
	source := strings.Replace(relationSource, "import (", "import (\n\"context\"\n\"github.com/weiloon1234/Foundry-Go/database\"", 1)
	source += `
var _ query.CreateDraft[Friendship]=FriendshipDraft{}
func writes(ctx context.Context,db database.Transactor,a,b User){
 r:=UserRelations().Friends.WithWriteLimit(10)
 _,_=r.Attach(ctx,db,a,b,FriendshipDraft{}.SetID(3))
 _,_=r.Detach(ctx,db,a,b)
 _,_=r.ForceDetach(ctx,db,a,b)
}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("unchanged relation write generation changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "defaults_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/query";"github.com/weiloon1234/Foundry-Go/database/codec")
func TestDefaults(t *testing.T){
 defaults:=query.Change(query.Assign[Friendship]("friendships","from_id",codec.Signed[int](),4),query.Assign[Friendship]("friendships","to_id",codec.Signed[int](),8))
 draft:=FriendshipDraft{}.SetID(1).SetToID(12)
 mutation,err:=draft.FoundryCreateMutation(defaults);if err!=nil{t.Fatal(err)}
 values,err:=query.ReadMutation(mutation);if err!=nil{t.Fatal(err)}
 from,err:=query.MutationValue[Friendship,int](values,"friendships","from_id");if v,ok:=from.Get();err!=nil||!ok||v!=4{t.Fatal("default lost",err)}
 to,err:=query.MutationValue[Friendship,int](values,"friendships","to_id");if v,ok:=to.Get();err!=nil||!ok||v!=12{t.Fatal("explicit input overwritten",err)}
 if draft.FromID().IsSet(){t.Fatal("original draft changed")}
 bad:=query.Change(query.Assign[Friendship]("friendships","unknown",codec.Signed[int](),2))
 if _,err:=draft.FoundryCreateMutation(bad);err==nil{t.Fatal("unknown default ignored")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated relation draft: %v\n%s", err, output)
	}
}

func TestRelationWriteDefaultsPreserveUUIDAndDistinctInputs(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/model")
//foundry:model table=links
type Link struct{ID model.ID[Link];Owner int}
type OwnerInput struct{Value int}
func(Link)MutateOwner(input OwnerInput)(int,error){return input.Value,nil}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "defaults_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/query";"github.com/weiloon1234/Foundry-Go/database/codec";"github.com/weiloon1234/Foundry-Go/model")
func TestDefaults(t *testing.T){
 key,err:=model.NewID[Link]();if err!=nil{t.Fatal(err)}
 defaults:=query.Change(query.Assign[Link]("links","id",codec.ID[Link](),key),query.Assign[Link]("links","owner",codec.Signed[int](),4))
 if _,err:=(LinkDraft{}).FoundryCreateMutation(defaults);err==nil{t.Fatal("invented a distinct mutation input")}
 mutation,err:=LinkDraft{}.SetOwner(OwnerInput{4}).FoundryCreateMutation(defaults);if err!=nil{t.Fatal(err)}
 values,err:=query.ReadMutation(mutation);if err!=nil{t.Fatal(err)}
 id,err:=query.MutationValue[Link,model.ID[Link]](values,"links","id");if v,ok:=id.Get();err!=nil||!ok||v!=key{t.Fatal("automatic UUID replaced a supplied default",err)}
 owner,err:=query.MutationValue[Link,OwnerInput](values,"links","owner");if v,ok:=owner.Get();err!=nil||!ok||v.Value!=4{t.Fatal("distinct input was transformed early",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated relation defaults: %v\n%s", err, output)
	}
}

func TestRelationWriteDraftReservedName(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:model table=users primary=ID\ntype User struct{ID int;FoundryCreateMutation string}")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("conflicting generated method accepted")
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid declaration published files")
	}
}
