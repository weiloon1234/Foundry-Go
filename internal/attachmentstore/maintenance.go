package attachmentstore

import "github.com/weiloon1234/Foundry-Go/database/query"

func Index(q FileQuery) query.ProjectionQuery[File, FileIndex] {
	f := FileFields()
	return SelectFileIndex(q, FileIndexSelection[File]{ID: f.ID.Value(), Owner: f.Owner.Value(), Scope: f.Scope.Value(), SubjectKey: f.SubjectKey.Value(), Identity: f.Identity.Value(), Collection: f.Collection.Value(), Locale: f.Locale.Value(), State: f.State.Value(), Disk: f.Disk.Value(), ObjectKey: f.ObjectKey.Value(), UpdatedAt: f.UpdatedAt.Value()})
}
