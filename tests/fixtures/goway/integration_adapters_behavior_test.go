package goway_test

import (
	"errors"
	e "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestORMExtensionDeclaration(t *testing.T) {
	if err := orm.Check(e.Articles, e.Authors); err != nil {
		t.Fatal(err)
	}
	if err := e.Owner.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := e.Declaration().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, slot := range e.ArticleExtensions.All() {
		g := &goldenCapture{}
		db := readDB(t, g)
		if _, err := orm.LoadFrom(t.Context(), db, e.Articles.Query().With(slot), []e.Article{{ID: 1}}); !errors.Is(err, fault.Missing) {
			t.Fatal("unbound runtime", err)
		}
		if g.statements != 0 {
			t.Fatal("unbound loader executed SQL")
		}
	}
	other := orm.IdentityOf(e.Articles.Table, e.Articles.Key())
	pinned := extensions.DefineORMOwner("articles", other, extensions.OwnerOptions{StorageModel: "extension_articles"})
	a, err := e.Owner.Subject(extensions.ORMIdentity(e.Owner).Reference(1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := pinned.Subject(other.ReferenceOf(e.Article{ID: 1}))
	if err != nil || a.Key != b.Key || a.Scope != b.Scope {
		t.Fatal("persisted owner identity", err)
	}
}
