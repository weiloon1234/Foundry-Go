package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestInsertSelectGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("context";"strings";"time";"github.com/weiloon1234/Foundry-Go/database";"github.com/weiloon1234/Foundry-Go/value")
//foundry:model table=records primary=ID
type Record struct{ID int64;Name string;Note value.Nullable[string];CreatedAt time.Time;UpdatedAt time.Time}
//foundry:model table=archives primary=ID
type Archive struct{ID int64;Name string;Note value.Nullable[string];Tag string;CreatedAt time.Time;UpdatedAt time.Time}
func(Archive)MutateTag(v string)(string,error){return strings.TrimSpace(v),nil}
func examples(ctx context.Context,db database.Transactor){
 f:=RecordFields()
 insert:=InsertArchiveFrom(QueryRecords().Where(f.ID.Gt(2))).SelectID(f.ID.Value()).SelectName(f.Name.Value()).SelectNote(f.Note.Value()).SelectCreatedAt(f.CreatedAt.Value()).Values(ArchiveDraft{}.SetTag(" batch "))
 var count int64;count,_=insert.Exec(ctx,db);_=count
 var rows []Archive;rows,_=insert.Returning(ctx,db,10);_=rows
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, data := range first {
		text := string(data)
		if strings.Contains(text, "SelectTag(") || strings.Contains(text, "SelectUpdatedAt(") {
			t.Fatal("generated insert selection can bypass a mutator or managed timestamp")
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("unchanged insert selection changed output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated insert selections: %v\n%s", err, output)
	}
}
