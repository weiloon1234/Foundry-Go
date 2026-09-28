package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestModelHooksGeneration(t *testing.T) {
	source := `package sample
import (
 "context"
 "github.com/weiloon1234/Foundry-Go/database"
 "github.com/weiloon1234/Foundry-Go/database/lifecycle"
 "github.com/weiloon1234/Foundry-Go/value"
)
type hooks string
type ctx string
type tx string
type operation string
type mutation string
type values string
type draft string
type current string
type zero string
type allHooks string
type factories string
type factory string
type observerSet string
type registrar string
type pool string
type observer string
type construct string
//foundry:model table=users primary=Key hooks=userHooks
type User struct{ Key int; Name string; Nickname value.Nullable[string]; Hooks hooks; Ctx ctx; Tx tx; Operation operation; Mutation mutation; Values values; Draft draft; Current current; Zero zero }
func userHooks() UserHooks {
 return UserHooks{
  Creating:func(ctx context.Context,tx *database.Tx,draft *UserDraft)error{*draft=draft.SetName("default");return nil},
  Updating:func(ctx context.Context,tx *database.Tx,before User,draft *UserDraft)error{return nil},
  Updated:func(ctx context.Context,tx *database.Tx,changes UserChanges)error{_ = changes.Fields().Name.Changed();return nil},
  AfterCommit:func(ctx context.Context,operation lifecycle.Operation,changes UserChanges)error{return nil},
 }
}

`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if !strings.Contains(first["user_foundry.gen.go"], "WithObserverHooks") {
		t.Fatal("hooks factory was not connected to writes")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatalf("hook generation is not reproducible: %v", err)
	}
	write(t, dir, "models.go", strings.Replace(source, "Creating:", "Creatng:", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
		t.Fatal("misspelled callback accepted")
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("invalid hooks replaced current output")
	}
}

func TestModelHooksRejectInvalidFactory(t *testing.T) {
	for name, declaration := range map[string]string{
		"missing":  "",
		"variable": "var userHooks = func()UserHooks{return UserHooks{}}",
		"argument": "func userHooks(int)UserHooks{return UserHooks{}}",
		"generic":  "func userHooks[T any]()UserHooks{return UserHooks{}}",
		"result":   "func userHooks()int{return 0}",
		"callback": "func userHooks()UserHooks{return UserHooks{Creating:func(*UserDraft)error{return nil}}}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:model table=users primary=Key hooks=userHooks\ntype User struct{Key int}\n"+declaration)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid factory accepted")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid factory published output")
			}
		})
	}
}

func TestModelObserversGenerateWithoutLocalFactoryAndReserveNames(t *testing.T) {
	source := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database"
 "github.com/weiloon1234/Foundry-Go/foundation"
)
//foundry:model table=users primary=Key
type User struct{Key int; Name string}
func register(r *foundation.Registrar,pool foundation.Key[*database.DB])error{
 return RegisterUserObserver(r,pool,NewUserObserver("audit"),func(foundation.Resolver)(func()UserHooks,error){return func()UserHooks{return UserHooks{}},nil})
}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if !strings.Contains(first["user_foundry.gen.go"], "WithObserverHooks") || !strings.Contains(first["user_foundry.gen.go"], "false)") {
		t.Fatal("unannotated model has no registered-observer adapter")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("observer output is not reproducible", err)
	}
	for _, name := range []string{"UserObserver", "NewUserObserver", "RegisterUserObserver"} {
		write(t, dir, "models.go", source+"\ntype "+name+" int\n")
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("observer symbol collision accepted", name)
		}
		if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
			t.Fatal("collision replaced published output")
		}
	}
}
