package generate

import (
	"reflect"
	"strings"
	"testing"
)

const aggregateSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/database/relation"
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:model table=users primary=ID
type User struct { ID int; ParentID value.Nullable[int]; Children relation.Many[User]; ChildCount relation.Value[int64]; ChildSum relation.Value[value.Nullable[decimal.Decimal]] }
func(User)DefineRelations()UserRelationSet { return UserRelationSet{Children:query.HasMany(UserFields().ID,UserFields().ParentID)} }
func(User)DefineAggregates()UserAggregateSet {
 return UserAggregateSet{
  ChildCount:query.Related(UserRelations().Children,query.Count[User]()),
  ChildSum:query.Related(UserRelations().Children,UserFields().ID.Sum()),
 }
}
`

func TestFreshAggregateGeneration(t *testing.T) {
	dir := fixture(t, aggregateSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	if !strings.Contains(output, "func UserAggregates()") || strings.Contains(output, "SetChildCount(") || !strings.Contains(output, "AggregateRelation[User, int64]") {
		t.Fatal("computed fields lost generated typing or entered persistence")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("aggregate generation was not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidAggregateGenerationDoesNotPublish(t *testing.T) {
	for _, source := range []string{
		strings.Replace(aggregateSource, "ChildCount relation.Value[int64]", "ChildCount relation.Value[int64] `foundry:\"column=count\"`", 1),
		strings.Replace(aggregateSource, "ChildCount relation.Value[int64]", "ChildCount relation.Value[string]", 1),
		strings.Replace(aggregateSource, "DefineAggregates()", "WrongMethod()", 1),
		strings.Replace(aggregateSource, "UserFields().ID.Sum()", "UserFields().ParentID.Eq(1)", 1),
	} {
		dir := fixture(t, source)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("invalid aggregate generated")
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid aggregate published output")
		}
	}
}
