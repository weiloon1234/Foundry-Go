package generate

import (
	"reflect"
	"strings"
	"testing"
)

const joinedGenerationSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/value"
)
type FoundryScope string
type scope string
type buyer struct{}
type sponsor struct{}
//foundry:model table=users primary=ID
type User struct{ID int; Name scope; Parent value.Nullable[int]}
//foundry:projection
type Row struct{Name scope; ParentName value.Nullable[scope]}
func Report() {
 a:=query.As[buyer](QueryUsers(),"buyer")
 b:=query.As[sponsor](QueryUsers(),"sponsor")
 af,bf:=UserFieldsAt(a.Scope()),UserFieldsAt(b.Scope())
 j:=query.LeftJoin(a,b,query.On(af.Parent,bf.ID))
 l:=UserFieldsAt(query.LeftScope(j,a.Scope()))
 r:=UserNullableFieldsAt(query.NullableRightScope(j,b.Scope()))
 _=ProjectRow(j).SelectName(l.Name.Value()).SelectParentName(r.Name.Value()).Query()
}
`

func TestFreshTypedJoinGenerationAndScopeNames(t *testing.T) {
	dir := fixture(t, joinedGenerationSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"UserScopedFieldSet[FoundryScope1 any]", "scope1 foundryquery.ModelScope[FoundryScope1, User]", "UserNullableFieldSet[FoundryScope1 any]"} {
		if !strings.Contains(first["user_foundry.gen.go"], want) {
			t.Fatal("scope declaration shadowed consumer symbols", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("scoped generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidOuterScopeCannotPublishGeneratedFiles(t *testing.T) {
	for _, source := range []string{
		strings.Replace(joinedGenerationSource, "UserNullableFieldsAt(query.NullableRightScope", "UserFieldsAt(query.NullableRightScope", 1),
		strings.Replace(joinedGenerationSource, "SelectParentName(r.Name.Value())", "SelectName(r.Name.Value())", 1),
		strings.Replace(joinedGenerationSource, "query.NullableRightScope(j,b.Scope())", "query.RightScope(j,b.Scope())", 1),
	} {
		dir := fixture(t, source)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("unsafe outer scope generated")
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid outer declaration published files")
		}
	}
}
