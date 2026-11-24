package modelguide

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestGuideDeclarations(t *testing.T) {
	if err := orm.Check(Products.Table, Articles.Table, Events.Table, Accounts.Table, Archives.Table); err != nil {
		t.Fatal(err)
	}
	slug, err := (Article{}).MutateSlug(SlugInput{Text: " Article "})
	if err != nil || slug != "article" {
		t.Fatalf("slug: %q, %v", slug, err)
	}
	id, err := model.ParseID[Article]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	article := Article{ID: id}
	reference, err := RestoreArticle(article)
	if err != nil || reference.Key() != article.ID {
		t.Fatalf("identity: %v", err)
	}
	if err := articleHooks().Deleting(t.Context(), nil, Article{Slug: "protected"}); !errors.Is(err, fault.Conflict) {
		t.Fatalf("protected deletion: %v", err)
	}
	if ArticleOwner.StorageModel() != "guide_articles" {
		t.Fatal("owner storage name changed")
	}
}
