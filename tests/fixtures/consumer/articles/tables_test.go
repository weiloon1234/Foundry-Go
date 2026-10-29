package articles_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/articles"
	"foundry.test/consumer/internal/cleanupfixture"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	baseline := cleanupfixture.Read(t)
	var oldIdentity model.Identity
	if err := json.Unmarshal(baseline.Identity, &oldIdentity); err != nil {
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
	if !bytes.Equal(before, after) || !oracleEqual(t, "consumer/articles/tables_test/2", oracle.Snapshot(after)) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := articles.ArticleIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement := baseline.Statements["read"]
	nextStatement, err := orm.Compile(articles.Articles.Query().Where(orm.TextEq(articles.Articles.Slug, "welcome")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments, nextStatement.Arguments()) || !oracleEqual(t, "consumer/articles/tables_test/1", oracle.Snapshot([]any{nextStatement.SQL(), nextStatement.Arguments()})) {
		t.Fatal("stored field read changed", oldStatement.SQL, nextStatement.SQL())
	}
}

func TestORMExtensionFoundationParity(t *testing.T) {
	baseline := cleanupfixture.Read(t)
	next := articles.ORMExtensionDeclaration()
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if baseline.Description != fmt.Sprintf("%#v", next.Describe()) || !oracleEqual(t, "consumer/articles/tables_test/3", oracle.Snapshot(fmt.Sprintf("%#v", next.Describe()))) {
		t.Fatal("extension owner or slot manifest changed")
	}
	id, err := model.ParseID[articles.Article]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	prior := *baseline.Subject
	current, err := articles.ArticleOwner.Subject(articles.ArticleIdentity.Reference(id))
	if err != nil || !reflect.DeepEqual(prior, current) || !oracleEqual(t, "consumer/articles/tables_test/4", oracle.Snapshot(current)) {
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
