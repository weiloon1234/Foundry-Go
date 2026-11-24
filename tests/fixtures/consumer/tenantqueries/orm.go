package tenantqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// DocumentLinks declares tenant-scoped document targets. Example: Documents.Query().With(DocumentLinks).
var DocumentLinks = orm.ManyToManyOf(Documents.Tags, orm.JoinKeys(Documents.Key(), DocumentTags.DocumentID), orm.JoinKeys(DocumentTags.TagCode, Tags.Key()))

// TagLinks retains document scopes when loaded from a tag. Example: Tags.Query().With(TagLinks).
var TagLinks = orm.ManyToManyOf(Tags.Documents, orm.JoinKeys(Tags.Key(), DocumentTags.TagCode), orm.JoinKeys(DocumentTags.DocumentID, Documents.Key()))
