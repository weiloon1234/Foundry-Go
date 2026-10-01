package articles_test

import (
	"context"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Owner.ActiveSubjects returns each reference's subject key in order, exactly
// as SubjectKey derives it, beside the active set, which excludes soft-deleted
// owners. Batch readers match their owners by position with it.
func TestOwnerActiveSubjectsFollowReferenceOrder(t *testing.T) {
	f := startApplication(t)
	first, second := f.createArticle(t, "first"), f.createArticle(t, "second")
	if err := f.store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := articles.QueryArticles().Delete(ctx, tx, second.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	owner := articles.ArticleExtensionOwner()
	references := []model.Reference[articles.Article, model.ID[articles.Article]]{second.FoundryReference(), first.FoundryReference(), first.FoundryReference()}
	var active map[string]bool
	var subjects []string
	if err := f.store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		var err error
		active, subjects, err = owner.ActiveSubjects(ctx, tx, f.store.Registry(), references)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(subjects) != len(references) {
		t.Fatal("subject keys are not index-aligned", len(subjects))
	}
	for i, reference := range references {
		key, err := owner.SubjectKey(reference)
		if err != nil || subjects[i] != key {
			t.Fatal("subject key differs from SubjectKey", i, err)
		}
	}
	if active[subjects[0]] || !active[subjects[1]] || len(active) != 1 {
		t.Fatal("the active set must hold only the owner that is not soft-deleted")
	}
}
