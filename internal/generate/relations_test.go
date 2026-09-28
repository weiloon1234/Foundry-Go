package generate

import (
	"reflect"
	"strings"
	"testing"
)

const relationSource = `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/database/relation"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:model table=users primary=ID
type User struct{ ID int; ParentID value.Nullable[int]; Parent relation.One[User]; Children relation.Many[User]; Friends relation.Through[User, Friendship] }
//foundry:model table=friendships primary=ID
type Friendship struct { ID int; FromID int; ToID int }
func(User)DefineRelations()UserRelationSet{
 return UserRelationSet{
  Parent:query.BelongsTo(UserFields().ParentID,UserFields().ID),
  Children:query.HasMany(UserFields().ID,UserFields().ParentID),
  Friends:query.ManyToMany(UserFields().ID,FriendshipFields().FromID,FriendshipFields().ToID,UserFields().ID),
 }
}
func typedRelationshipFilters() {
 var builder UserQuery = QueryUsers().WhereHas(UserRelations().Children.Where(UserFields().ID.Gt(2))).WhereDoesntHave(UserRelations().Friends)
 _ = builder.Where(UserRelations().Parent.Exists()).With(UserRelations().Children)
}
`

func TestFreshTypedRelationGeneration(t *testing.T) {
	dir := fixture(t, relationSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	if !strings.Contains(output, "func UserRelations()") || !strings.Contains(output, "func (User) FoundryQuery()") || !strings.Contains(output, "ThroughRelation[User, User, Friendship]") || strings.Contains(output, "SetParent(") || strings.Contains(output, "SetFriends(") {
		t.Fatal("relation was not separated from persisted model fields")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("relation generation changed unchanged output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRelationDeclarationsDoNotPublish(t *testing.T) {
	for _, source := range []string{
		strings.Replace(relationSource, "Parent relation.One[User]", "Parent relation.One[*User]", 1),
		strings.Replace(relationSource, "Through[User, Friendship]", "Through[User, *Friendship]", 1),
		strings.Replace(relationSource, "Through[User, Friendship]", "Through[Friendship, User]", 1),
		strings.Replace(relationSource, "Parent relation.One[User]", "Parent relation.One[User] `foundry:\"column=parent\"`", 1),
		strings.Replace(relationSource, "Parent:query.BelongsTo(UserFields().ParentID,UserFields().ID)", "Parent:query.HasMany(UserFields().ID,UserFields().ParentID)", 1),
		strings.Replace(relationSource, "func(User)DefineRelations()UserRelationSet", "func(User)MisspelledDefinition()UserRelationSet", 1),
	} {
		dir := fixture(t, source)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("invalid relation generation succeeded")
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid relation published files")
		}
	}
}
