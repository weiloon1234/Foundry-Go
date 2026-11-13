package articles

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin publish-file-sources
type MediaPublication struct {
	Logo    value.Optional[attachments.Result[Article, model.ID[Article]]]
	Gallery []attachments.Result[Article, model.ID[Article]]
}

func (s *Service) PublishMedia(ctx context.Context, article Article, input ArticleMedia) (MediaPublication, error) {
	var publication MediaPublication
	if logo, supplied := input.Logo.Get(); supplied {
		result, err := s.x.Logo.ReplaceFile(ctx, article, logo)
		publication.Logo = value.Set(result)
		if err != nil {
			return publication, err
		}
	}
	results, err := attachments.AddFilesTo(ctx, s.x.Galleries, article, input.Galleries)
	publication.Gallery = results
	return publication, err
}

// doc-example:end publish-file-sources

// MediaPublicationError keeps attempted publication outcomes available to the
// application's error boundary without exposing them through error formatting.
type MediaPublicationError struct {
	Publication MediaPublication
	cause       error
}

func (e *MediaPublicationError) Error() string { return "article media publication failed" }
func (e *MediaPublicationError) Unwrap() error { return e.cause }

// doc-example:begin publish-slot-file
func (s *Service) ReplaceLogo(ctx context.Context, visible orm.Query[Article], article Article, file attachments.FileSource, slug string) (attachments.Result[Article, model.ID[Article]], error) {
	prepared, err := s.x.Logo.PrepareFile(ctx, article, file)
	result := attachments.Result[Article, model.ID[Article]]{Operation: prepared.Operation()}
	if err != nil {
		return result, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		locked, err := orm.RequireLocked(ctx, tx, orm.Lock(visible.Where(orm.KeyEq(Articles.Key(), article.ID))))
		if err != nil {
			return err
		}
		if _, err := orm.Update(ctx, tx, visible, Articles.Key(), locked.ID, orm.TextSet(Articles.Slug, slug)); err != nil {
			return err
		}
		result, err = s.x.Logo.ReplaceIn(ctx, tx, locked, prepared)
		return err
	})
	// Publication inside the callback remains subject to the outer commit.
	// Preserve both the result and transaction error for reconciliation.
	return result, err
}

// doc-example:end publish-slot-file

// doc-example:begin append-slot-file
func (s *Service) AddGalleryFile(ctx context.Context, article Article, file attachments.FileSource) (attachments.Result[Article, model.ID[Article]], error) {
	return s.x.Galleries.AddFile(ctx, article, file)
}

func (s *Service) AddPreparedGallery(ctx context.Context, tx *database.Tx, article Article, prepared attachments.Prepared[Article, model.ID[Article]]) (attachments.Result[Article, model.ID[Article]], error) {
	return s.x.Galleries.AddIn(ctx, tx, article, prepared)
}

// doc-example:end append-slot-file

// doc-example:begin transaction-current-files
func CurrentFiles(ctx context.Context, tx *database.Tx, manager *attachments.Manager, bound ArticleSlotHandles, article Article) (value.Optional[attachments.Attachment[Article, model.ID[Article]]], []attachments.Attachment[Article, model.ID[Article]], error) {
	owner := ArticleIdentity.ReferenceOf(article)
	logo, err := attachments.OneCollection(bound.Logo).FirstIn(ctx, tx, manager, owner)
	if err != nil {
		return logo, nil, err
	}
	gallery, err := attachments.ManyCollection(bound.Galleries).ListIn(ctx, tx, manager, owner)
	return logo, gallery, err
}

// doc-example:end transaction-current-files

// doc-example:begin clear-slot-files
func (s *Service) ClearFiles(ctx context.Context, tx *database.Tx, visible orm.Query[Article], id model.ID[Article]) error {
	article, err := orm.RequireLocked(ctx, tx, orm.Lock(visible.Where(orm.KeyEq(Articles.Key(), id))))
	if err != nil {
		return err
	}
	if _, err := s.x.Logo.ClearIn(ctx, tx, article); err != nil {
		return err
	}
	_, err = s.x.Galleries.ClearIn(ctx, tx, article)
	return err
}

// doc-example:end clear-slot-files

// doc-example:begin reconcile-files
func ReconcileFiles(ctx context.Context, manager *attachments.Manager) ([]attachments.ReconcileResult, error) {
	return manager.ReconcilePending(ctx, 100)
}

// doc-example:end reconcile-files

// doc-example:begin pinned-video-download
// VideoDownload defers opening until after the handler authorizes article/id.
func VideoDownload(videos attachments.Collection[Article, model.ID[Article]], manager *attachments.Manager, article Article, id attachments.ID[Article]) foundryhttp.Download {
	owner := ArticleIdentity.ReferenceOf(article)
	return foundryhttp.DownloadFrom(func(ctx context.Context) (foundryhttp.DownloadContent, error) {
		body, file, err := videos.OpenSeek(ctx, manager, owner, id)
		if err != nil {
			if errors.Is(err, database.NotFound) {
				return foundryhttp.DownloadContent{}, foundryhttp.NotFound.WithCause(err)
			}
			return foundryhttp.DownloadContent{}, storagehttp.ResponseError(err)
		}
		return foundryhttp.DownloadContent{
			Body:      body,
			Name:      file.Info().OriginalName,
			MediaType: foundryhttp.MediaType(file.Info().MediaType),
			Modified:  file.CreatedAt().UTC(),
			EntityTag: foundryhttp.EntityTag(`"` + file.ID().String() + `"`),
		}, nil
	})
}

// doc-example:end pinned-video-download
