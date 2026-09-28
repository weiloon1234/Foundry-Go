package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestModelMutatorGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type Email = string
type Helper string
func(Helper)MutateDomain(d UserDraft)UserDraft{return d}
//foundry:model table=users primary=Key
type User struct{ Key int; Email Email; Nickname value.Nullable[string]; Helper Helper }
func (User) MutateEmail(v Email)(Email,error){ return v,nil }
func (User) MutateNickname(v string)(string,error){ return v,nil }
var initial = UserDraft{}.SetEmail("input").ClearNickname()
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	for _, want := range []string{"NewMutatedModelField", "NewNullableMutatedModelField", "(User{}).MutateEmail", "(User{}).MutateNickname"} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated field registration missing %s", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatalf("mutator generation is not reproducible: %v", err)
	}
	write(t, dir, "models.go", strings.Replace(`package sample
//foundry:model table=users primary=Key
type User struct{Key int; Email string}
func (User) MutateEmail(v string)(string,error){return v,nil}
`, "MutateEmail", "MutateEamil", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "no persisted field") {
		t.Fatalf("misspelled automatic mutator was ignored: %v", err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("invalid mutator replaced previous generated output")
	}
}

func TestModelMutatorRejectsInvalidSignatures(t *testing.T) {
	for name, method := range map[string]string{
		"input":            "func(User)MutateEmail(v any)(string,error){return \"\",nil}",
		"output":           "func(User)MutateEmail(v string)(int,error){return 0,nil}",
		"missing error":    "func(User)MutateEmail(v string)string{return v}",
		"concrete error":   "func(User)MutateEmail(v string)(string,*problem){return v,nil}",
		"pointer receiver": "func(*User)MutateEmail(v string)(string,error){return v,nil}",
		"variadic":         "func(User)MutateEmail(v ...string)(string,error){return \"\",nil}",
		"no inputs":        "func(User)MutateEmail()(string,error){return \"\",nil}",
		"named value":      "func(User)MutateEmail(v OtherEmail)(OtherEmail,error){return v,nil}",
		"nullable wrapper": "func(User)MutateNickname(v value.Nullable[string])(value.Nullable[string],error){return v,nil}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type OtherEmail string
type problem struct{}
func(*problem)Error()string{return "problem"}
//foundry:model table=users primary=Key
type User struct{Key int; Email string; Nickname value.Nullable[string]}
`+method)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "mutator requires a value receiver and signature") {
				t.Fatalf("invalid mutator signature accepted: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid mutator published files")
			}
		})
	}
}
