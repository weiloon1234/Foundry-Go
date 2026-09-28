package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestModelChangesGeneration(t *testing.T) {
	source := `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type before string
type after string
type assigned string
type changes string
type err string
type item string
type snapshot string
type present string
type defaults string
//foundry:model table=users primary=Key
type User struct {
 Key int
 Before before
 After after
 Assigned assigned
 Changed changes
 Err err
 Item item
 Snapshot snapshot
 Present present
 Defaults defaults
 Nickname value.Nullable[string]
 Transient []string ` + "`foundry:\"-\"`" + `
}
var _ UserChanges
func inspect() (UserChanges,error) {
 result,err := CompareUser(value.Set(User{Key:1}),value.Set(User{Key:1}),UserDraft{}.ClearNickname())
 _ = result.Fields().Before.Changed()
 _ = result.Fields().After.Assigned()
 var _ value.Optional[value.Nullable[string]] = result.Fields().Nickname.After()
 return result,err
}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	for _, want := range []string{"type UserChanges struct", "type UserFieldChanges struct", "func CompareUser(", "CompareModelField", "foundryUserSnapshot"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing generated changes declaration %s", want)
		}
	}
	if strings.Contains(output, "Transient:") || strings.Contains(output, "FieldChange[[]string]") {
		t.Fatal("ignored state entered persisted changes")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatalf("model changes are not reproducible: %v", err)
	}
	write(t, dir, "models.go", source+"\ntype UserChanges struct{}\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("handwritten/generated change symbol collision accepted")
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("invalid changes generation replaced existing output")
	}
}
