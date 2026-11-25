package tenantqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func syncDocumentTags(ctx context.Context, db *database.DB, document Document, goTag, sqlTag Tag, DocumentLinks orm.ThroughRelation[Document, Tag, DocumentTag], DocumentTags DocumentTagTable) error {
	// doc-example:begin model-relation-writes-3
	tags := DocumentLinks
	changes, err := orm.Sync(ctx, db, tags, document, []Tag{goTag, sqlTag},
		[]orm.Assignment[DocumentTag]{orm.Set(DocumentTags.Weight, 1)},
		[]orm.Assignment[DocumentTag]{orm.Set(DocumentTags.Weight, 5)})
	if err != nil {
		return err
	}
	// doc-example:end model-relation-writes-3
	_ = changes
	return err
}
