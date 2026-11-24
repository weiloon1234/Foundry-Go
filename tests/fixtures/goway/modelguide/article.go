package modelguide

import (
	"context"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

type SlugInput struct{ Text string }

//foundry:model table=guide_articles var=Articles key=ID hooks=articleHooks
type Article struct {
	ID        model.ID[Article]
	Slug      string `foundry:"input=SlugInput"`
	Revision  int
	DeletedAt value.Nullable[time.Time] `foundry:"role=soft-delete"`
	Title     translations.Text
	Cover     attachments.One[Article]
}

func (Article) MutateSlug(input SlugInput) (string, error) {
	return strings.ToLower(strings.TrimSpace(input.Text)), nil
}

func articleHooks() orm.Hooks[Article] {
	return orm.Hooks[Article]{Deleting: func(_ context.Context, _ *database.Tx, article Article) error {
		if article.Slug == "protected" {
			return fault.Conflict
		}
		return nil
	}}
}
