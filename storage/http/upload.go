// Package http connects typed HTTP file inputs and responses to storage disks.
// Authorization and file validation remain the consuming endpoint's policies.
package http

import (
	"context"
	"errors"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// StoreUpload persists a captured request-owned upload before the request ends.
// It uses the supplied key, never the client's filename. Existing HTTP upload
// limits, validation, bounded spooling and request cleanup remain in force.
// Detected media type is a default only; it is not proof of a valid file format.
// A successful StoredObject can be retained; UploadedFile cannot outlive requests.
func StoreUpload(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, file foundryhttp.UploadedFile, options storage.PutOptions) (storage.StoredObject, error) {
	if err := disk.Validate(); err != nil {
		return storage.StoredObject{}, err
	}
	if err := key.Validate(); err != nil {
		return storage.StoredObject{}, err
	}
	if file.IsZero() {
		return storage.StoredObject{}, storage.Failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	if size, supplied := options.Size.Get(); supplied && size != file.Size() {
		return storage.StoredObject{}, storage.Failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	options.Size = value.Set(file.Size())
	if options.ContentType == "" {
		options.ContentType = storage.MediaType(file.ContentType())
	}
	body, err := file.Open(ctx)
	if err != nil {
		return storage.StoredObject{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	result, err := disk.Put(ctx, key, body, options)
	cleanup := body.Close()
	if cleanup != nil {
		state, code := storage.Applied, storage.Unavailable
		var prior *storage.Error
		if err != nil {
			state = storage.Unknown
			if errors.As(err, &prior) {
				state, code = prior.Outcome(), prior.Code()
			}
		}
		combined := storage.Failure(code, storage.PutOperation, state, errors.Join(err, cleanup))
		if prior != nil {
			if id, ok := prior.Cleanup().Get(); ok {
				combined = combined.WithCleanup(id)
			}
		}
		return storage.StoredObject{}, combined
	}
	return result, err
}
