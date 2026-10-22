package generate

import (
	"strings"
	"testing"
)

func TestModelGlobalScopeSourceGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/query"
//foundry:model table=users primary=Key
type User struct{ Key int; Active bool }
func defineUserGlobalScopes() []query.GlobalScope[User] {
	return []query.GlobalScope[User]{query.NewGlobalScope("active", UserFields().Active.Eq(true))}
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	// The function is passed uncalled so scopes are built lazily, after the
	// model's memoized query declaration exists.
	if output := generatedSnapshot(t, dir)["user_foundry.gen.go"]; !strings.Contains(output, ".WithGlobalScopeSource(defineUserGlobalScopes)") {
		t.Fatal("generated query does not register the lazy global scope source")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelGlobalScopeSourceRejectsInvalidSignatures(t *testing.T) {
	for name, method := range map[string]string{
		"other model":   "func defineUserGlobalScopes()[]query.GlobalScope[Other]{return nil}",
		"single scope":  "func defineUserGlobalScopes()query.GlobalScope[User]{return query.GlobalScope[User]{}}",
		"defined slice": "type Scopes []query.GlobalScope[User]\nfunc defineUserGlobalScopes()Scopes{return nil}",
		"arguments":     "func defineUserGlobalScopes(tenant int)[]query.GlobalScope[User]{return nil}",
		"variadic":      "func defineUserGlobalScopes(extra ...query.GlobalScope[User])[]query.GlobalScope[User]{return extra}",
		"error result":  "func defineUserGlobalScopes()([]query.GlobalScope[User],error){return nil,nil}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/query"
type Other struct{}
//foundry:model table=users primary=Key
type User struct{Key int}
`+method)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "function defineUserGlobalScopes requires signature func defineUserGlobalScopes() []query.GlobalScope[User]") {
				t.Fatalf("invalid defineUserGlobalScopes was not reported: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid defineUserGlobalScopes published output")
			}
		})
	}
}

func TestModelGlobalScopeSourceRejectsOldMethodForm(t *testing.T) {
	for name, method := range map[string]string{
		"value receiver":   "func (User) DefineGlobalScopes() []query.GlobalScope[User] { return nil }",
		"pointer receiver": "func (*User) DefineGlobalScopes() []query.GlobalScope[User] { return nil }",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/query"
//foundry:model table=users primary=Key
type User struct{Key int}
`+method)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "model User declares DefineGlobalScopes as a method; declare func defineUserGlobalScopes() []query.GlobalScope[User] instead (methods on model types make every importing package compile the model's query engine)") {
				t.Fatalf("old DefineGlobalScopes method was not rejected with migration diagnostic: %v", err)
			}
		})
	}
}
