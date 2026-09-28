package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModelMutationInputImportedNames(t *testing.T) {
	for _, name := range []string{"d", "defaults", "values", "err", "v", "scope", "FoundryScope"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import (
    raw "foundry.test/generator/input"
    "github.com/weiloon1234/Foundry-Go/database/query"
    "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:model table=users primary=Key
type User struct{Key int;Name value.Nullable[string]}
func(User)MutateName(v raw.Input)(string,error){return v.Value,nil}
var _ = UserDraft{}.ClearName().SetName(raw.Input{})
var _ = query.OnConflict(UserFields().Key).DoUpdate(UserFields().Name.Set(raw.Input{}))
`)
			input := filepath.Join(dir, "input")
			if err := os.Mkdir(input, 0755); err != nil {
				t.Fatal(err)
			}
			write(t, input, "input.go", "package "+name+"\ntype Input struct{Value string}\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal("input import collided with generated local", err)
			}
			if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelMutationInputPreservesJSONAndUUIDCapabilities(t *testing.T) {
	dir := fixture(t, `package sample
import (
    "context"
    "github.com/weiloon1234/Foundry-Go/database"
    "github.com/weiloon1234/Foundry-Go/database/query"
    "github.com/weiloon1234/Foundry-Go/model"
    "github.com/weiloon1234/Foundry-Go/value"
)
type FreshID struct{Value model.ID[User]}
type Preferences struct{Theme string;Tags []string}
//foundry:model table=users
type User struct{ID model.ID[User];Settings value.JSON[Preferences];Backup value.Nullable[value.JSON[Preferences]]}
func(User)MutateID(input FreshID)(model.ID[User],error){return input.Value,nil}
func(User)MutateSettings(input Preferences)(value.JSON[Preferences],error){return value.NewJSON(input)}
func(User)MutateBackup(input Preferences)(value.JSON[Preferences],error){return value.NewJSON(input)}
type other struct{}
func use(ctx context.Context,db database.Transactor,id model.ID[User],stored value.JSON[Preferences]) {
    f,q:=UserFields(),QueryUsers()
    draft:=UserDraft{}.SetID(FreshID{id}).SetSettings(Preferences{}).ClearBackup()
    _,_ = q.Create(ctx,db,draft)
    _ = q.Where(f.ID.Eq(id),f.Settings.Contains(stored),f.Settings.Properties().Theme.Scalar().Like("%go%"),f.Backup.Properties().Tags.At(-1).Scalar().Eq("go"))
    _ = query.OnConflict(f.ID).DoUpdate(f.Settings.Set(Preferences{}),f.Backup.SetNull())
    aliased:=query.As[other](q,"other")
    fields:=UserFieldsAt(aliased.Scope())
    _ = fields.Settings.Properties().Theme.Scalar().Eq("go")
    _ = query.OnConflict(fields.ID).DoUpdate(fields.Settings.Set(Preferences{}),fields.Backup.Incoming())
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal("input wrappers lost JSON or UUID capabilities", err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("current input wrapper check changed generated output")
	}
}

const mutationInputFixture = `package sample
import (
    "context"
    "fmt"
    "github.com/weiloon1234/Foundry-Go/database"
    "github.com/weiloon1234/Foundry-Go/database/query"
    "github.com/weiloon1234/Foundry-Go/value"
)
type Key int
type RawKey struct{Value int}
type FreshName struct{Value string}
type StoredName string
//foundry:model table=users primary=Key hooks=writeHooks
type User struct{Key Key; Name StoredName; Note value.Nullable[StoredName]}
func(User)MutateKey(v RawKey)(Key,error){return Key(v.Value),nil}
func(User)MutateName(v FreshName)(StoredName,error){return StoredName(v.Value),nil}
func(User)MutateNote(v FreshName)(StoredName,error){return StoredName(v.Value),nil}
func writeHooks()UserHooks{return UserHooks{Creating:func(_ context.Context,_ *database.Tx,d *UserDraft)error{
    input,ok:=d.Name().Get();if ok{*d=d.SetName(input)};return nil
}}}
var draft=UserDraft{}.SetKey(RawKey{1}).SetName(FreshName{"private"}).ClearNote()
var _ fmt.Formatter = draft
var _ value.Optional[FreshName] = draft.Name()
var _ value.Optional[value.Nullable[FreshName]] = draft.Note()
var _ = UserFields().Name.Eq(StoredName("stored"))
var _ = UserFields().Key.Eq(Key(1))
var _ = query.OnConflict(UserFields().Key).DoUpdate(UserFields().Name.Set(FreshName{"fresh"}),UserFields().Note.SetNull())
var _ = UserFields().Name.Incoming()
var _ = query.On(UserFields().Key,UserFields().Key)
var _ = QueryUsers().Where(UserFields().Name.Like(StoredName("%name%")))
var _ = query.SelectValue(QueryUsers(),UserFields().Name.Value())
`

func TestModelMutationInputGeneration(t *testing.T) {
	dir := fixture(t, mutationInputFixture)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	for _, want := range []string{"NewInputModelField", "NewNullableInputModelField", "AssignInput[User]", "UserNameInputField", "UserNameNullableInputField", "UserNoteNullableInputField", "SetName(v FreshName)", "Set(v FreshName)", "MutationValue[User, FreshName]", "(UserDraft) Format", "StoredName", "FieldChange[StoredName]"} {
		if !strings.Contains(output, want) {
			t.Fatal("missing typed input contract", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("input generation is not reproducible", err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`var bad=UserDraft{}.SetName(StoredName("stored"))`,
		`var bad=UserFields().Name.Set(StoredName("stored"))`,
		`var bad=UserFields().Name.Eq(FreshName{})`,
		`var bad value.Optional[StoredName] = UserDraft{}.Name()`,
		`var bad=UserDraft{}.SetKey(Key(1))`,
		`var bad=UserFields().Note.Set(value.Of(FreshName{}))`,
	} {
		write(t, dir, "invalid.go", "package sample\nimport \"github.com/weiloon1234/Foundry-Go/value\"\nvar _ = value.Set(1)\n"+invalid)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "cannot use") {
			t.Fatal("invalid typed input compiled or failed for an unrelated reason", invalid, err)
		}
		if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
			t.Fatal("invalid input changed published output")
		}
	}
}

func TestModelMutationInputRejectsInvalidDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"interface input": `//foundry:model table=users primary=Key
type User struct{Key int;Name string}
func(User)MutateName(v any)(string,error){return "",nil}`,
		"nullable input": `//foundry:model table=users primary=Key
type User struct{Key int;Name value.Nullable[string]}
func(User)MutateName(v value.Nullable[int])(string,error){return "",nil}`,
		"formatter field": `//foundry:model table=users primary=Key
type User struct{Key int;Format string}`,
		"input type collision": `type UserNameInputField struct{}
type FreshName struct{}
//foundry:model table=users primary=Key
type User struct{Key int;Name string}
func(User)MutateName(v FreshName)(string,error){return "",nil}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\nimport \"github.com/weiloon1234/Foundry-Go/value\"\nvar _ = value.Set(1)\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid input declaration accepted")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid declaration published generated files")
			}
		})
	}
}
