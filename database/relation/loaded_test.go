package relation_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type user struct {
	ID         int
	Introducer relation.One[user]
	Referrals  relation.Many[user]
}

func TestThroughKeepsPivotDataAndOwnsContainers(t *testing.T) {
	type pivot struct{ ID int }
	var missing relation.Through[user, pivot]
	if _, loaded := missing.Get(); loaded || missing.IsLoaded() {
		t.Fatal("zero through relation is loaded")
	}
	if _, loaded := relation.Linked([]relation.Link[user, pivot](nil)).Get(); !loaded {
		t.Fatal("empty links lost loaded state")
	}
	items := []relation.Link[user, pivot]{{Model: user{ID: 1}, Pivot: pivot{ID: 2}}, {Model: user{ID: 1}, Pivot: pivot{ID: 3}}}
	loaded := relation.Linked(items)
	items[0].Pivot.ID = 4
	first, ok := loaded.Get()
	if !ok || len(first) != 2 || first[0].Pivot.ID != 2 || first[1].Pivot.ID != 3 {
		t.Fatal("pivot rows collapsed or aliased")
	}
	first[0].Model.ID = 5
	second, _ := loaded.Get()
	if second[0].Model.ID != 1 {
		t.Fatal("retrieved link mutated stored collection")
	}
}

func TestLoadedStatesAndOwnedContainers(t *testing.T) {
	var missing relation.One[user]
	if _, loaded := missing.Get(); loaded {
		t.Fatal("zero relation is loaded")
	}
	empty := relation.Single(value.Optional[user]{})
	if model, loaded := empty.Get(); !loaded || model.IsSet() {
		t.Fatal("empty relation lost loaded state")
	}
	parent := user{ID: 1}
	one := relation.Single(value.Set(parent))
	parent.ID = 2
	model, loaded := one.Get()
	m, ok := model.Get()
	if !loaded || !ok || m.ID != 1 {
		t.Fatal("singular relation retained caller struct")
	}
	items := []user{{ID: 3}}
	many := relation.Collection(items)
	items[0].ID = 4
	first, loaded := many.Get()
	if !loaded || len(first) != 1 || first[0].ID != 3 {
		t.Fatal("collection aliased source")
	}
	first[0].ID = 5
	second, _ := many.Get()
	if second[0].ID != 3 {
		t.Fatal("retrieval mutated collection")
	}
	if _, loaded := relation.Collection([]user(nil)).Get(); !loaded {
		t.Fatal("empty collection is not loaded")
	}
}
