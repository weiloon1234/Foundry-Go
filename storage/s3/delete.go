package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// DeleteBatch implements storage.BatchDeleter. AWS uses one DeleteObjects
// request; R2 and compatible profiles delete sequentially because their
// DeleteObjects checksum support differs. Results align with keys. Deleting a
// key on a versioned bucket adds a delete marker, as a single Delete does.
func (b *Backend) DeleteBatch(ctx context.Context, keys []storage.ObjectKey) ([]error, error) {
	if err := b.ready(ctx, storage.DeleteOperation); err != nil {
		return nil, err
	}
	if len(keys) == 0 || len(keys) > storage.MaxBatchDelete {
		return nil, storage.Failure(storage.Invalid, storage.DeleteOperation, storage.Unchanged, nil)
	}
	full := make([]string, len(keys))
	for i, key := range keys {
		var err error
		if full[i], err = b.object(key); err != nil {
			return nil, err
		}
	}
	results := make([]error, len(keys))
	if b.config.Provider != AWS {
		for i, key := range keys {
			results[i] = callback.Invoke("S3 delete", func() error { return b.Delete(ctx, key, storage.DeleteOptions{}) })
		}
		return results, nil
	}
	objects := make([]types.ObjectIdentifier, len(keys))
	for i := range keys {
		objects[i] = types.ObjectIdentifier{Key: aws.String(full[i])}
	}
	output, err := b.client.DeleteObjects(ctx, &awss3.DeleteObjectsInput{Bucket: aws.String(b.config.Bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(false)}})
	if err != nil {
		return nil, failure(storage.DeleteOperation, storage.Unknown, err)
	}
	if output == nil {
		return nil, storage.Failure(storage.IntegrityFailed, storage.DeleteOperation, storage.Unknown, nil)
	}
	index := make(map[string]int, len(keys))
	for i, key := range full {
		index[key] = i
	}
	reported := make([]bool, len(keys))
	for _, deleted := range output.Deleted {
		if i, ok := index[aws.ToString(deleted.Key)]; ok {
			reported[i] = true
		}
	}
	for _, failed := range output.Errors {
		i, ok := index[aws.ToString(failed.Key)]
		if !ok {
			continue
		}
		reported[i] = true
		code, outcome := storage.Unavailable, storage.Unknown
		switch aws.ToString(failed.Code) {
		case "AccessDenied":
			code, outcome = storage.Forbidden, storage.Unchanged
		case "NoSuchKey":
			continue
		}
		results[i] = storage.Failure(code, storage.DeleteOperation, outcome, nil)
	}
	for i, ok := range reported {
		if !ok {
			results[i] = storage.Failure(storage.IntegrityFailed, storage.DeleteOperation, storage.Unknown, nil)
		}
	}
	return results, nil
}

var _ storage.BatchDeleter = (*Backend)(nil)
