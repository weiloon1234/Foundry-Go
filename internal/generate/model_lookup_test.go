package generate

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestLookupWriteGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("context";"github.com/weiloon1234/Foundry-Go/database";"github.com/weiloon1234/Foundry-Go/model")
//foundry:model table=records
type Record struct{ID model.ID[Record];Name string}
func examples(ctx context.Context,db database.Transactor){
 q:=QueryRecords().Where(RecordFields().Name.Eq("record"))
 var result Record
 result,_=q.FirstOrCreate(ctx,db,RecordDraft{}.SetName("record"))
 result,_=q.UpdateOrCreate(ctx,db,RecordDraft{}.SetName("record"),func(ctx context.Context,tx *database.Tx,current Record)(RecordDraft,error){return RecordDraft{}.SetName(current.Name+"!"),nil})
 _=result
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
		t.Fatal("unchanged lookup write output changed")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated lookup writes: %v\n%s", err, output)
	}
}
