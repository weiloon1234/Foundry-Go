package attachments

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"io"
	"mime"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// FileSource is an upload the framework can open and read, such as
// foundryhttp.UploadedFile, which satisfies it without attachments importing
// HTTP. An operation opens it, reads within the policy's MaxBytes and closes
// its reader before returning; the caller keeps owning the underlying file.
// Detected bytes decide acceptance: Name is display metadata and
// ClientContentType only the existing text-specialization hint.
type FileSource = upload.Source

// replaceFile replaces owner's collection with file, publishing in the
// manager's own transaction, so commit the owner first; inspect
// Result.Publication when an error is returned. To publish inside a
// transaction, PrepareFile before it begins and ReplaceIn inside it.
func (s slotDefinition[M, K, S]) replaceFile(ctx context.Context, owner M, file FileSource) (Result[M, K], error) {
	return withFile(ctx, file, func(upload Upload) (Result[M, K], error) { return s.replace(ctx, owner, upload) })
}

// replace replaces owner's collection with one upload.
func (s slotDefinition[M, K, S]) replace(ctx context.Context, owner M, upload Upload) (Result[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return Result[M, K]{}, err
	}
	return s.collection.Replace(ctx, m, s.binding.Reference(owner), upload)
}

// prepareFile reads, checks and stores file for owner without publishing it,
// so a later transaction publishes it with ReplaceIn (or AddIn) using only its
// own connection. Call it before that transaction begins; owner may be a model
// the transaction creates, with its key chosen first. See Collection.Prepare.
func (s slotDefinition[M, K, S]) prepareFile(ctx context.Context, owner M, file FileSource) (Prepared[M, K], error) {
	return withFile(ctx, file, func(upload Upload) (Prepared[M, K], error) { return s.prepare(ctx, owner, upload) })
}

// prepare is PrepareFile for one upload.
func (s slotDefinition[M, K, S]) prepare(ctx context.Context, owner M, upload Upload) (Prepared[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return Prepared[M, K]{}, err
	}
	return s.collection.Prepare(ctx, m, s.binding.Reference(owner), upload)
}

// replaceIn publishes a prepared upload as owner's collection inside tx, a
// transaction of the extension store's pool, using only that transaction; a
// rollback unpublishes it. Old files are cleaned after tx commits. See
// Collection.AddIn.
func (s slotDefinition[M, K, S]) replaceIn(ctx context.Context, tx *database.Tx, owner M, prepared Prepared[M, K]) (Result[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return Result[M, K]{}, err
	}
	return s.collection.ReplaceIn(ctx, tx, m, s.binding.Reference(owner), prepared)
}

// discard reclaims a prepared upload that will not be published.
func (s slotDefinition[M, K, S]) discard(ctx context.Context, prepared Prepared[M, K]) error {
	m, err := s.bound()
	if err != nil {
		return err
	}
	return s.collection.Discard(ctx, m, prepared)
}

// detach retires one file of owner's collection and cleans its storage.
func (s slotDefinition[M, K, S]) detach(ctx context.Context, owner M, id ID[M]) (ChangeResult, error) {
	m, err := s.bound()
	if err != nil {
		return ChangeResult{}, err
	}
	return s.collection.Detach(ctx, m, s.binding.Reference(owner), id)
}

// clear retires every file of owner's collection and cleans their storage.
func (s slotDefinition[M, K, S]) clear(ctx context.Context, owner M) (ChangeResult, error) {
	m, err := s.bound()
	if err != nil {
		return ChangeResult{}, err
	}
	return s.collection.Clear(ctx, m, s.binding.Reference(owner))
}

// accepts reports whether file satisfies this slot's policy, using the same
// filename, size and byte-detected media checks as a write, including image
// inspection under an image plan, without storage or database work. It
// returns false for a rejected file and an error only when checking fails.
// Image transformation limits are checked by the write, which stays
// authoritative. Wrap Accepts in validation.Custom with an application rule ID
// to report rejected uploads as request validation issues.
func (s slotDefinition[M, K, S]) accepts(ctx context.Context, file FileSource) (bool, error) {
	m, err := s.bound()
	if err != nil {
		return false, err
	}
	if file == nil {
		return false, invalid()
	}
	policy := s.collection.definition.policy
	if !validFilename(filename.StripInvisible(file.Name())) {
		return false, nil
	}
	// The upload is buffered like a write, so it shares the write admission
	// that bounds concurrent buffers and drains on Close.
	accepted := false
	err = m.calls.Run(ctx, "attachment acceptance", func(ctx context.Context) error {
		reader, err := file.Open(ctx)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(workscope.Reader(ctx, reader), policy.MaxBytes+1))
		if err = joinFailures(err, reader.Close()); err != nil {
			return err
		}
		if int64(len(body)) > policy.MaxBytes {
			return nil
		}
		if _, err := acceptMedia(ctx, m, policy, body, hintOf(file)); err != nil {
			if callback.ErrorIs(err, fault.Invalid) {
				return nil
			}
			return err
		}
		accepted = true
		return nil
	})
	return accepted, err
}

// addFile appends file to owner's multiple-file collection.
func (s manySlot[M, K]) addFile(ctx context.Context, owner M, file FileSource) (Result[M, K], error) {
	return withFile(ctx, file, func(upload Upload) (Result[M, K], error) { return s.add(ctx, owner, upload) })
}

// add appends one upload to owner's multiple-file collection.
func (s manySlot[M, K]) add(ctx context.Context, owner M, upload Upload) (Result[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return Result[M, K]{}, err
	}
	return s.collection.Add(ctx, m, s.binding.Reference(owner), upload)
}

// addIn publishes a prepared upload into owner's collection inside tx; see
// ReplaceIn.
func (s manySlot[M, K]) addIn(ctx context.Context, tx *database.Tx, owner M, prepared Prepared[M, K]) (Result[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return Result[M, K]{}, err
	}
	return s.collection.AddIn(ctx, tx, m, s.binding.Reference(owner), prepared)
}

// reorder sets the collection order to an exact permutation of its file IDs.
func (s manySlot[M, K]) reorder(ctx context.Context, owner M, order []ID[M]) ([]Attachment[M, K], error) {
	m, err := s.bound()
	if err != nil {
		return nil, err
	}
	return s.collection.Reorder(ctx, m, s.binding.Reference(owner), order)
}

func checkAddFiles[M any, K comparable](slot slotDefinition[M, K, Many[M]], count int) error {
	if _, err := slot.bound(); err != nil {
		return err
	}
	if count > slot.collection.definition.policy.MaxFiles {
		return invalid()
	}
	return nil
}

func addFiles[F FileSource, R any](ctx context.Context, files []F, validate func(int) error, add func(context.Context, FileSource) (R, error)) ([]R, error) {
	if err := validate(len(files)); err != nil {
		return nil, err
	}
	results := make([]R, 0, len(files))
	for _, file := range files {
		result, err := add(ctx, file)
		results = append(results, result)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// withFile opens file for one operation and closes it after the operation
// returns; a close failure is reported beside the operation's result.
func withFile[T any](ctx context.Context, file FileSource, run func(Upload) (T, error)) (T, error) {
	if file == nil {
		return *new(T), invalid()
	}
	reader, err := file.Open(ctx)
	if err != nil {
		return *new(T), err
	}
	result, err := run(Upload{Source: reader, OriginalName: file.Name(), ContentType: hintOf(file)})
	return result, joinFailures(err, reader.Close())
}

// hintOf keeps a client content type only when it is a valid media type,
// dropping parameters such as charset; the hint never decides acceptance.
func hintOf(file FileSource) storage.MediaType {
	parsed, _, err := mime.ParseMediaType(file.ClientContentType())
	if err != nil {
		return ""
	}
	hint := storage.MediaType(parsed)
	if hint.Validate() != nil {
		return ""
	}
	return hint
}
