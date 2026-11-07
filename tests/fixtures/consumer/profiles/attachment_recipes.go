package profiles

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// doc-example:begin prepare-collection-file
func (s Services) PrepareDocument(ctx context.Context, profile Profile, file attachments.FileSource) (attachments.Prepared[Profile, model.ID[Profile]], error) {
	reader, err := file.Open(ctx)
	if err != nil {
		return attachments.Prepared[Profile, model.ID[Profile]]{}, err
	}
	defer reader.Close()
	return Documents.Prepare(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), attachments.Upload{
		Source: reader, OriginalName: file.Name(), ContentType: storage.MediaType(file.ClientContentType()),
	})
}

// doc-example:end prepare-collection-file

// doc-example:begin publish-collection-file
func (s Services) AddDocumentIn(ctx context.Context, tx *database.Tx, profile Profile, prepared attachments.Prepared[Profile, model.ID[Profile]]) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.AddIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), prepared)
}

func (s Services) ReplaceDocumentsIn(ctx context.Context, tx *database.Tx, profile Profile, prepared attachments.Prepared[Profile, model.ID[Profile]]) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.ReplaceIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), prepared)
}

// doc-example:end publish-collection-file

// doc-example:begin add-collection-file
func (s Services) AddDocument(ctx context.Context, profile Profile, upload attachments.Upload) (attachments.Result[Profile, model.ID[Profile]], error) {
	return Documents.Add(ctx, s.Attachments, ProfileIdentity.ReferenceOf(profile), upload)
}

// doc-example:end add-collection-file

// doc-example:begin retire-collection-files
func (s Services) DetachDocumentIn(ctx context.Context, tx *database.Tx, profile Profile, id attachments.ID[Profile]) (attachments.ChangeResult, error) {
	return Documents.DetachIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile), id)
}

func (s Services) ClearDocumentsIn(ctx context.Context, tx *database.Tx, profile Profile) (attachments.ChangeResult, error) {
	return Documents.ClearIn(ctx, tx, s.Attachments, ProfileIdentity.ReferenceOf(profile))
}

// doc-example:end retire-collection-files
