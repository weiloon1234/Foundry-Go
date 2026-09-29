package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModelSoftDeleteGeneration(t *testing.T) {
	source := `package sample
import (
 "context"
 "time"
 "github.com/weiloon1234/Foundry-Go/database"
 "github.com/weiloon1234/Foundry-Go/database/lifecycle"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:model table=users hooks=userHooks
type User struct {
 ID model.ID[User]
 CreatedAt time.Time
 UpdatedAt temporal.DateTime
 DeletedAt value.Nullable[time.Time] DELETED
}
//foundry:model table=entries primary=ID soft_deletes=true
type Entry struct {ID int;DeletedAt value.Nullable[temporal.DateTime]}
func(User)MutateDeletedAt(v time.Time)(time.Time,error){return v,nil}
func userHooks()UserHooks {
 return UserHooks{
  Restoring:func(context.Context,*database.Tx,User)error{return nil},
  ForceDeleting:func(context.Context,*database.Tx,User)error{return nil},
  Deleted:func(_ context.Context,_ *database.Tx,changes UserChanges)error{var _ value.Optional[lifecycle.Operation]=changes.Operation();return nil},
  Restored:func(context.Context,*database.Tx,UserChanges)error{return nil},
  ForceDeleted:func(context.Context,*database.Tx,UserChanges)error{return nil},
 }
}
var _ func(context.Context,database.Transactor,model.ID[User])(User,error)=QueryUsers().WithTrashed().ForceDelete
var _ func(context.Context,database.Transactor,model.ID[User])(User,error)=QueryUsers().OnlyTrashed().Restore
var _ = QueryUsers().OnlyTrashed().WithoutTrashed().Where(UserFields().DeletedAt.IsNull())
var _ = UserDraft{}.SetDeletedAt(time.Time{}).ClearDeletedAt()
var _ func(context.Context,*database.Tx,model.ID[User])(value.Optional[User],error)=QueryUsers().ForUpdate().WithTrashed().OnlyTrashed().WithoutTrashed().Find
`
	source = strings.ReplaceAll(source, "DELETED", "`foundry:\"column=removed_on\"`")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{`.WithSoftDeletes("removed_on")`, ".WithTimestamps(", "Managed soft-delete timestamp", "case lifecycle.SoftDelete:", "case lifecycle.ForceDelete:"} {
		if !strings.Contains(first["user_foundry.gen.go"], want) {
			t.Fatal("missing deletion lifecycle contract", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("soft-delete generation changed on repeat")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelSoftDeleteOptOutPreservesOrdinaryFields(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:model table=users primary=ID soft_deletes=false\ntype User struct{ID int;DeletedAt string}")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["user_foundry.gen.go"]
	if strings.Contains(output, ".WithSoftDeletes(") || strings.Contains(output, "Managed soft-delete timestamp") || strings.Contains(output, "func (q UserQuery) ForceDelete(") {
		t.Fatal("opted-out fields acquired automatic deletion behavior")
	}
}

func TestModelSoftDeleteNoticesFollowOptOut(t *testing.T) {
	dir := fixture(t, `package sample
import("time";"github.com/weiloon1234/Foundry-Go/value")
//foundry:model table=users primary=ID
type User struct {
 ID int
 // Retained deletion history.
 DeletedAt value.Nullable[time.Time]
}
func(u User)AccessDeletedAt()(bool,error){return !u.DeletedAt.IsNull(),nil}
func(User)MutateDeletedAt(v time.Time)(time.Time,error){return v,nil}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.go")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "Managed soft-delete timestamp") {
		t.Fatal("actual field lacks deletion notice")
	}
	write(t, dir, "models.go", strings.Replace(string(first), "table=users primary=ID", "table=users primary=ID soft_deletes=false", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err == nil {
		t.Fatal("opt-out did not invalidate generated behavior")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "Managed soft-delete timestamp") {
		t.Fatal("opt-out retained obsolete managed notice")
	}
	for _, want := range []string{"Retained deletion history.", "Custom getter", "Custom setter"} {
		if !strings.Contains(string(after), want) {
			t.Fatal("opt-out removed unrelated field documentation", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelSoftDeleteRejectsInvalidDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"option":         "//foundry:model table=users primary=ID soft_deletes=yes\ntype User struct{ID int}",
		"missing":        "//foundry:model table=users primary=ID soft_deletes=true\ntype User struct{ID int}",
		"not nullable":   "//foundry:model table=users primary=ID\ntype User struct{ID int;DeletedAt time.Time}",
		"wrong type":     "//foundry:model table=users primary=ID\ntype User struct{ID int;DeletedAt value.Nullable[string]}",
		"ignored":        "//foundry:model table=users primary=ID soft_deletes=true\ntype User struct{ID int;DeletedAt value.Nullable[time.Time] OMIT}",
		"distinct input": "//foundry:model table=users primary=ID\ntype User struct{ID int;DeletedAt value.Nullable[time.Time]}\ntype Input struct{T time.Time}\nfunc(User)MutateDeletedAt(v Input)(time.Time,error){return v.T,nil}",
	} {
		t.Run(name, func(t *testing.T) {
			source = strings.ReplaceAll(source, "OMIT", "`foundry:\"-\"`")
			dir := fixture(t, "package sample\nimport(\"time\";\"github.com/weiloon1234/Foundry-Go/value\")\nvar _=time.Time{}\nvar _=value.Set(1)\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid soft-delete declaration generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid declaration published output")
			}
		})
	}
}
