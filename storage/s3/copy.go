package s3

import (
	"context"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// maxCopyBytes is the single-request CopyObject limit. Larger copies stream.
const maxCopyBytes int64 = 5 << 30

// Copy implements storage.ServerCopier with one CopyObject pinned to the
// source's ETag (and version). It declines as Unsupported/Unchanged before any
// mutation when the streamed path is required: a destination condition, a
// source above 5 GiB, a declared checksum the source metadata cannot prove, or
// a compatible profile that has not declared ServerCopy. CopyObject is a
// mutation and is never retried automatically.
func (b *Backend) Copy(ctx context.Context, source, target storage.ObjectKey, options storage.CopyOptions, maximum int64) (storage.ObjectInfo, error) {
	declined := storage.Failure(storage.Unsupported, storage.CopyOperation, storage.Unchanged, nil)
	if err := b.ready(ctx, storage.CopyOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	sourceFull, err := b.object(source)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	targetFull, err := b.object(target)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	write := options.Destination
	if b.config.Provider == Compatible && !b.config.Compatible.ServerCopy || write.Condition.RequiresAbsence() || write.Condition.Match() != "" || options.Source.Range.IsSet() {
		return storage.ObjectInfo{}, declined
	}
	if err := b.readOptions(options.Source); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := write.Validate(b.config.MaxObjectBytes); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.Capabilities().ValidatePut(write); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.validateMetadata(write.Metadata); err != nil {
		return storage.ObjectInfo{}, err
	}
	info, err := b.Stat(ctx, source, options.Source)
	if err != nil {
		return storage.ObjectInfo{}, unchanged(err)
	}
	if info.Size > min(maximum, b.config.MaxObjectBytes) {
		return storage.ObjectInfo{}, storage.Failure(storage.LimitExceeded, storage.CopyOperation, storage.Unchanged, nil)
	}
	if info.Size > maxCopyBytes {
		return storage.ObjectInfo{}, declined
	}
	if size, supplied := write.Size.Get(); supplied && size != info.Size {
		return storage.ObjectInfo{}, storage.Failure(storage.Invalid, storage.CopyOperation, storage.Unchanged, nil)
	}
	checksum, known := info.Checksum.Get()
	if expected, declared := write.Checksum.Get(); declared {
		if !known {
			return storage.ObjectInfo{}, declined
		}
		if expected != checksum {
			return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.CopyOperation, storage.Unchanged, nil)
		}
	}
	if write.ContentType == "" {
		write.ContentType = info.ContentType
	}
	// REPLACE always writes the same metadata a streamed copy would: the
	// destination content type, the proven checksum and the requested
	// metadata. Source cache, disposition and custom headers are not inherited.
	described := fields(write, info.Checksum)
	input := &awss3.CopyObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(targetFull), CopySource: aws.String(copySource(b.config.Bucket, sourceFull, info.Version)), CopySourceIfMatch: aws.String(string(info.ETag)), StorageClass: described.storageClass, ServerSideEncryption: described.encryption, SSEKMSKeyId: described.kmsKey, MetadataDirective: types.MetadataDirectiveReplace, ContentType: described.contentType, Metadata: described.metadata, CacheControl: described.cacheControl, ContentDisposition: described.disposition, ContentEncoding: described.encoding}
	result, err := b.client.CopyObject(ctx, input)
	if err != nil {
		return storage.ObjectInfo{}, failure(storage.CopyOperation, storage.Unknown, err)
	}
	if result == nil || result.CopyObjectResult == nil || result.CopyObjectResult.LastModified == nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.CopyOperation, storage.Applied, nil)
	}
	modified, err := temporal.NewDateTime(*result.CopyObjectResult.LastModified)
	if err != nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.CopyOperation, storage.Applied, err)
	}
	copied := storage.ObjectInfo{Key: target, Size: info.Size, ContentType: write.ContentType, Modified: modified, ETag: storage.ETag(aws.ToString(result.CopyObjectResult.ETag)), Version: b.versionID(aws.ToString(result.VersionId)), Checksum: value.Optional[storage.SHA256]{}}
	if known {
		copied.Checksum = value.Set(checksum)
	}
	if copied.ETag == "" || copied.Validate() != nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.CopyOperation, storage.Applied, nil)
	}
	return copied, nil
}

// copySource encodes bucket/key once for x-amz-copy-source, including '+'.
func copySource(bucket, key string, version storage.VersionID) string {
	encoded := strings.ReplaceAll((&url.URL{Path: bucket + "/" + key}).EscapedPath(), "+", "%2B")
	if version != "" {
		encoded += "?versionId=" + url.QueryEscape(string(version))
	}
	return encoded
}

// unchanged reports a failed pre-copy read as a copy that changed nothing.
func unchanged(err error) error {
	if failed, ok := err.(*storage.Error); ok {
		return storage.Failure(failed.Code(), storage.CopyOperation, storage.Unchanged, err)
	}
	return failure(storage.CopyOperation, storage.Unchanged, err)
}

var _ storage.ServerCopier = (*Backend)(nil)
