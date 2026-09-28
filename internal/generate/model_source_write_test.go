package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestSourceWriteGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("context";"strings";"time";"github.com/weiloon1234/Foundry-Go/database";"github.com/weiloon1234/Foundry-Go/database/query";"github.com/weiloon1234/Foundry-Go/value")
//foundry:model table=records primary=ID
type Record struct{ID int64;Name string;Note value.Nullable[string];Tag string;CreatedAt time.Time;UpdatedAt time.Time;DeletedAt value.Nullable[time.Time]}
func(Record)MutateTag(v string)(string,error){return strings.TrimSpace(v),nil}
func examples(ctx context.Context,db database.Transactor){
 f:=RecordFields();q:=QueryRecords()
 update:=UpdateRecordFrom(q.Where(f.Name.Ne("excluded")),q.Limit(3)).MatchID(f.ID.Value()).SelectName(f.Name.Value()).SelectNote(f.Note.Value()).Values(RecordDraft{}.SetTag(" tag "))
 var count int64;count,_=update.Exec(ctx,db);_=count
 var rows []Record;rows,_=update.Returning(ctx,db,10);_=rows
 _,_=DeleteRecordUsing(q,q).MatchNullableID(query.Nullable(f.ID.Value())).Exec(ctx,db)
 _,_=ForceDeleteRecordUsing(q.WithTrashed(),q.WithTrashed()).MatchID(f.ID.Value()).Returning(ctx,db,10)
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, data := range first {
		text := string(data)
		for _, bad := range []string{" SelectID(", " SelectTag(", " SelectUpdatedAt("} {
			// Restrict to the update builder: insertion can legitimately select IDs.
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "RecordUpdateFromBuilder[") && strings.Contains(line, bad) {
					t.Fatal("unsafe update source selector", line)
				}
			}
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("source write generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated source writes: %v\n%s", err, output)
	}
}
