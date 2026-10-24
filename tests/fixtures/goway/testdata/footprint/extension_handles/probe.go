package probe

import (
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

type Article struct {
	ID      int64
	Title   translations.Text
	Cover   attachments.One[Article]
	Gallery attachments.Many[Article]
	SEO     metadata.Value[string]
}
type ArticleTable struct {
	orm.Table[Article, int64]
	Title   orm.Slot[Article, translations.Text]
	Cover   orm.Slot[Article, attachments.One[Article]]
	Gallery orm.Slot[Article, attachments.Many[Article]]
	SEO     orm.Slot[Article, metadata.Value[string]]
}

var Articles = orm.Define("articles", func(b orm.Builder[Article], m *Article) ArticleTable {
	return ArticleTable{orm.TableOf(b, &m.ID), orm.SlotOf(b, &m.Title), orm.SlotOf(b, &m.Cover), orm.SlotOf(b, &m.Gallery), orm.SlotOf(b, &m.SEO)}
})
