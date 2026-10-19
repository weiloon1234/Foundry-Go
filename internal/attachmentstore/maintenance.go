package attachmentstore

import "github.com/weiloon1234/Foundry-Go/database/query"

func Index(q FileQuery) query.ProjectionQuery[File, FileIndex] {
	f := FileFields()
	return SelectFileIndex(q, FileIndexSelection[File]{ID: query.Value(f.ID), Owner: query.Value(f.Owner), Scope: query.Value(f.Scope), SubjectKey: query.Value(f.SubjectKey), Identity: query.Value(f.Identity), Collection: query.Value(f.Collection), Locale: query.Value(f.Locale), State: query.Value(f.State), Disk: query.Value(f.Disk), ObjectKey: query.Value(f.ObjectKey), UpdatedAt: query.Value(f.UpdatedAt)})
}
