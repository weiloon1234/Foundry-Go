package articles_test

import (
	"bytes"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestORMFoundationParity(t *testing.T) {
	if err := orm.Check(articles.Tables()...); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[articles.Article]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	record := articles.Article{ID: id}
	oldIdentity, err := record.FoundryReference().Identity()
	if err != nil {
		t.Fatal(err)
	}
	nextIdentity, err := articles.ArticleIdentity.ReferenceOf(record).Identity()
	if err != nil {
		t.Fatal(err)
	}
	before, err := oldIdentity.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	after, err := nextIdentity.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := articles.ArticleIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement, err := articles.QueryArticles().Where(articles.ArticleFields().Slug.Eq("welcome")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	nextStatement, err := orm.Compile(articles.Articles.Query().Where(articles.Articles.Slug.Eq("welcome")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL() != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments(), nextStatement.Arguments()) {
		t.Fatal("stored field read changed", oldStatement.SQL(), nextStatement.SQL())
	}
}

func TestORMExtensionFoundationParity(t *testing.T) {
	old := articles.ArticleExtensionDeclaration()
	next := articles.ORMExtensionDeclaration()
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Describe(), next.Describe()) {
		t.Fatal("extension owner or slot manifest changed")
	}
	id, err := model.ParseID[articles.Article]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	prior, err := articles.ArticleExtensionOwner().Subject(articles.ArticleExtensionOwner().Reference(id))
	if err != nil {
		t.Fatal(err)
	}
	current, err := articles.ArticleOwner.Subject(articles.ArticleIdentity.Reference(id))
	if err != nil || !reflect.DeepEqual(prior, current) {
		t.Fatal("extension storage identity changed", err)
	}
	for _, relation := range articles.ArticleSlots.All() {
		if _, err := orm.Compile(articles.Articles.Query().With(relation)); !errors.Is(err, fault.Missing) {
			t.Fatal("unbound runtime accepted", err)
		}
	}
	if _, err := orm.Compile(articles.Articles.Query().With(articles.ArticleAuthor)); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Compile(articles.Authors.Query().With(articles.AuthorArticles)); err != nil {
		t.Fatal(err)
	}
}
