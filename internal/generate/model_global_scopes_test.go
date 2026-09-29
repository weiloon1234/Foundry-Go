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
func (User) DefineGlobalScopes() []query.GlobalScope[User] {
	return []query.GlobalScope[User]{query.NewGlobalScope("active", UserFields().Active.Eq(true))}
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	// The method value is passed uncalled so scopes are built lazily, after the
	// model's memoized query declaration exists.
	if output := generatedSnapshot(t, dir)["user_foundry.gen.go"]; !strings.Contains(output, ".WithGlobalScopeSource((User{}).DefineGlobalScopes)") {
		t.Fatal("generated query does not register the lazy global scope source")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelGlobalScopeSourceRejectsInvalidSignatures(t *testing.T) {
	for name, method := range map[string]string{
		"pointer receiver": "func(*User)DefineGlobalScopes()[]query.GlobalScope[User]{return nil}",
		"other model":      "func(User)DefineGlobalScopes()[]query.GlobalScope[Other]{return nil}",
		"single scope":     "func(User)DefineGlobalScopes()query.GlobalScope[User]{return query.GlobalScope[User]{}}",
		"defined slice":    "type Scopes []query.GlobalScope[User]\nfunc(User)DefineGlobalScopes()Scopes{return nil}",
		"arguments":        "func(User)DefineGlobalScopes(tenant int)[]query.GlobalScope[User]{return nil}",
		"variadic":         "func(User)DefineGlobalScopes(extra ...query.GlobalScope[User])[]query.GlobalScope[User]{return extra}",
		"error result":     "func(User)DefineGlobalScopes()([]query.GlobalScope[User],error){return nil,nil}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/database/query"
type Other struct{}
//foundry:model table=users primary=Key
type User struct{Key int}
`+method)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "DefineGlobalScopes requires a value receiver and signature func (User) DefineGlobalScopes() []query.GlobalScope[User]") {
				t.Fatalf("invalid DefineGlobalScopes was not reported: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid DefineGlobalScopes published output")
			}
		})
	}
}
