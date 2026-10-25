package attachments

import "context"

// AddFilesTo appends files through a bound ORM many-slot handle in input order.
// The slot and batch limit are checked before any file opens. Results include
// every attempted file, including the first failure; earlier publications remain
// published. Inspect each Result.Publication and never retry unknown outcomes.
// Example: results, err := attachments.AddFilesTo(ctx, bound.Galleries, article, files).
func AddFilesTo[F FileSource, M any, K comparable](ctx context.Context, slot ManyHandle[M, K], owner M, files []F) ([]Result[M, K], error) {
	if slot.core == nil {
		return nil, invalid()
	}
	return addFiles(ctx, files, slot.core.checkFiles, func(ctx context.Context, file FileSource) (Result[M, K], error) {
		return slot.AddFile(ctx, owner, file)
	})
}
