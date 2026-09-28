package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestModelRetrievalHooksGeneration(t *testing.T) {
	source := `package sample
import (
 "context"
 "github.com/weiloon1234/Foundry-Go/database"
 "github.com/weiloon1234/Foundry-Go/foundation"
)
type executor string
type item string
type hooks string
type allHooks string
type factory string
type factories string
type observerSet string
type registrar string
type pool string
type observer string
type construct string
//foundry:model table=users primary=Key hooks=writeHooks retrieval=readHooks
type User struct{ Key int; Name string; Executor executor; Item item }
func writeHooks() UserHooks { return UserHooks{} }
func readHooks() UserRetrievalHooks {
 return UserRetrievalHooks{Retrieved:func(ctx context.Context,executor database.Executor,item User)error{_,err:=QueryUsers().Count(ctx,executor);return err}}
}
func register(r *foundation.Registrar,pool foundation.Key[*database.DB])error{
 return RegisterUserRetrievalObserver(r,pool,NewUserRetrievalObserver("read.audit"),func(foundation.Resolver)(func()UserRetrievalHooks,error){return readHooks,nil})
}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, contract := range []string{"WithObserverHooks", "WithRetrievalHooks", "NewRetrievalObserver", "RetrievalObserverFactories"} {
		if !strings.Contains(first["user_foundry.gen.go"], contract) {
			t.Fatal("missing retrieval contract", contract)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("retrieval generation is not reproducible", err)
	}
	for _, replacement := range []struct{ old, new string }{
		{"Retrieved:", "Retreived:"},
		{"item User)error", "item int)error"},
		{"func()UserRetrievalHooks,error", "func()UserHooks,error"},
	} {
		write(t, dir, "models.go", strings.Replace(source, replacement.old, replacement.new, 1))
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("invalid retrieval contract accepted", replacement)
		}
		if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
			t.Fatal("invalid retrieval contract changed published output")
		}
	}
}

func TestModelRetrievalHooksRejectInvalidFactoriesAndReservedNames(t *testing.T) {
	for name, declaration := range map[string]string{
		"missing":  "",
		"variable": "var readHooks=func()UserRetrievalHooks{return UserRetrievalHooks{}}",
		"argument": "func readHooks(int)UserRetrievalHooks{return UserRetrievalHooks{}}",
		"generic":  "func readHooks[T any]()UserRetrievalHooks{return UserRetrievalHooks{}}",
		"result":   "func readHooks()UserHooks{return UserHooks{}}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:model table=users primary=Key retrieval=readHooks\ntype User struct{Key int}\n"+declaration)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid retrieval factory accepted")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid retrieval factory published output")
			}
		})
	}
	for _, name := range []string{"UserRetrievalHooks", "UserRetrievalObserver", "NewUserRetrievalObserver", "RegisterUserRetrievalObserver"} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:model table=users primary=Key\ntype User struct{Key int}\ntype "+name+" int\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("retrieval symbol collision accepted")
			}
		})
	}
}
