package generate

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestPerModelWriteGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("context";"time";"github.com/weiloon1234/Foundry-Go/database";"github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/value")
//foundry:model table=records
type Record struct{ID model.ID[Record];Name string;DeletedAt value.Nullable[time.Time]}
func examples(ctx context.Context,db database.Transactor){
 q:=QueryRecords()
 var rows []Record
 rows,_=q.CreateEach(ctx,db,[]RecordDraft{RecordDraft{}.SetName("first")})
 rows,_=q.Where(RecordFields().Name.Eq("first")).UpdateEach(ctx,db,5,func(ctx context.Context,tx *database.Tx,current Record)(RecordDraft,error){return RecordDraft{}.SetName(current.Name+"!"),nil})
 rows,_=q.DeleteEach(ctx,db,5)
 rows,_=q.RestoreEach(ctx,db,5)
 rows,_=q.WithTrashed().ForceDeleteEach(ctx,db,5)
 _=rows
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	initial := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(initial, generatedSnapshot(t, dir)) {
		t.Fatal("unchanged per-model write generation changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "batch_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/query";"github.com/weiloon1234/Foundry-Go/model")
func TestSharedPreparation(t *testing.T){
 drafts:=[]RecordDraft{RecordDraft{}.SetName("one"),RecordDraft{}.SetName("two")}
 mutations,err:=QueryRecords().foundryCreateMutations(drafts);if err!=nil{t.Fatal(err)}
 var ids []model.ID[Record]
 for i,mutation:=range mutations{
  if drafts[i].ID().IsSet(){t.Fatal("original UUID input changed")}
  fields,err:=query.ReadMutation(mutation);if err!=nil{t.Fatal(err)}
  captured,err:=query.MutationValue[Record,model.ID[Record]](fields,"records","id");if err!=nil{t.Fatal(err)}
  id,ok:=captured.Get();if !ok{t.Fatal("shared preparation omitted UUID")};ids=append(ids,id)
 }
 if len(ids)!=2||ids[0]==ids[1]{t.Fatal("batch lost separate row identities")}
 if _,err:=QueryRecords().foundryCreateMutations(make([]RecordDraft,query.MaxPerModelWriteRows+1));err==nil{t.Fatal("unbounded preparation accepted")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated per-model writes: %v\n%s", err, output)
	}
}
