package s3

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const checksumMetadata = "foundry-sha256"

func (b *Backend) info(key storage.ObjectKey, size *int64, media, tag, version *string, modified *time.Time, metadata map[string]string) (storage.ObjectInfo, error) {
	if size == nil || modified == nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	instant, err := temporal.NewDateTime(*modified)
	if err != nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, err)
	}
	info := storage.ObjectInfo{Key: key, Size: *size, ContentType: storage.MediaType(aws.ToString(media)), ETag: storage.ETag(aws.ToString(tag)), Version: b.versionID(aws.ToString(version)), Modified: instant}
	if info.ContentType == "" {
		info.ContentType = storage.Binary
	}
	if digest, ok := metadata[checksumMetadata]; ok {
		parsed, err := storage.ParseSHA256(digest)
		if err != nil {
			return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, err)
		}
		info.Checksum = value.Set(parsed)
	}
	if err := info.Validate(); err != nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, err)
	}
	if info.Size > b.config.MaxObjectBytes || info.ETag == "" {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	return info, nil
}
func (b *Backend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	if err := b.ready(ctx, storage.StatOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.readOptions(options); err != nil {
		return storage.ObjectInfo{}, err
	}
	if options.Range.IsSet() {
		return storage.ObjectInfo{}, storage.Failure(storage.Invalid, storage.StatOperation, storage.NotApplicable, nil)
	}
	input := &awss3.HeadObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}
	if options.IfMatch != "" {
		input.IfMatch = aws.String(string(options.IfMatch))
	}
	if options.Version != "" {
		input.VersionId = aws.String(string(options.Version))
	}
	result, err := b.client.HeadObject(ctx, input, b.readRetry)
	if err != nil {
		return storage.ObjectInfo{}, failure(storage.StatOperation, storage.NotApplicable, err)
	}
	if result == nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	info, err := b.info(key, result.ContentLength, result.ContentType, result.ETag, result.VersionId, result.LastModified, result.Metadata)
	if err != nil {
		return storage.ObjectInfo{}, failure(storage.StatOperation, storage.NotApplicable, err)
	}
	if options.IfMatch != "" && info.ETag != options.IfMatch || options.Version != "" && info.Version != options.Version {
		return storage.ObjectInfo{}, storage.Failure(storage.PreconditionFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	return info, nil
}
func (b *Backend) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	if err := b.ready(ctx, storage.OpenOperation); err != nil {
		return nil, storage.ReadInfo{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return nil, storage.ReadInfo{}, err
	}
	if err := b.readOptions(options); err != nil {
		return nil, storage.ReadInfo{}, err
	}
	input := &awss3.GetObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}
	if options.IfMatch != "" {
		input.IfMatch = aws.String(string(options.IfMatch))
	}
	if options.Version != "" {
		input.VersionId = aws.String(string(options.Version))
	}
	if span, ok := options.Range.Get(); ok {
		input.Range = aws.String("bytes=" + strconv.FormatInt(span.Offset, 10) + "-" + strconv.FormatInt(span.Offset+span.Length-1, 10))
	}
	result, err := b.client.GetObject(ctx, input, b.readRetry)
	if err != nil {
		if result != nil && result.Body != nil {
			err = errors.Join(err, result.Body.Close())
		}
		return nil, storage.ReadInfo{}, failure(storage.OpenOperation, storage.NotApplicable, err)
	}
	if result == nil {
		return nil, storage.ReadInfo{}, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	size := result.ContentLength
	offset, length := int64(0), aws.ToInt64(size)
	if span, ok := options.Range.Get(); ok {
		var total int64
		offset, length, total, err = parseContentRange(aws.ToString(result.ContentRange))
		if err == nil {
			expectedOffset, expectedLength, e := span.Resolve(total)
			if e != nil || offset != expectedOffset || length != expectedLength || result.ContentLength == nil || *result.ContentLength != length {
				err = storage.IntegrityFailed
			}
		}
		size = &total
	} else if aws.ToString(result.ContentRange) != "" {
		err = storage.IntegrityFailed
	}
	var info storage.ObjectInfo
	if err == nil {
		info, err = b.info(key, size, result.ContentType, result.ETag, result.VersionId, result.LastModified, result.Metadata)
	}
	if err == nil && (options.IfMatch != "" && info.ETag != options.IfMatch || options.Version != "" && info.Version != options.Version) {
		err = storage.PreconditionFailed
	}
	if result.Body == nil {
		err = storage.IntegrityFailed
	}
	if err != nil {
		if result.Body != nil {
			err = errors.Join(err, result.Body.Close())
		}
		return nil, storage.ReadInfo{}, failure(storage.OpenOperation, storage.NotApplicable, err)
	}
	return result.Body, storage.ReadInfo{Object: info, Offset: offset, Length: length}, nil
}
func parseContentRange(text string) (offset, length, total int64, err error) {
	invalid := func() (int64, int64, int64, error) { return 0, 0, 0, storage.IntegrityFailed }
	if !strings.HasPrefix(text, "bytes ") {
		return invalid()
	}
	span, size, ok := strings.Cut(strings.TrimPrefix(text, "bytes "), "/")
	if !ok {
		return invalid()
	}
	start, end, ok := strings.Cut(span, "-")
	if !ok {
		return invalid()
	}
	offset, e1 := strconv.ParseInt(start, 10, 64)
	last, e2 := strconv.ParseInt(end, 10, 64)
	total, e3 := strconv.ParseInt(size, 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || offset < 0 || last < offset || last >= total {
		return invalid()
	}
	return offset, last - offset + 1, total, nil
}
func (b *Backend) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
	if err := b.ready(ctx, storage.DeleteOperation); err != nil {
		return err
	}
	full, err := b.object(key)
	if err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	// S3 evaluates delete preconditions against the current object, not an
	// explicitly selected historical version; ValidateDelete rejects that
	// combination. A version selector alone deletes exactly that version.
	if options.Version == "null" {
		return storage.Failure(storage.Unsupported, storage.DeleteOperation, storage.Unchanged, nil)
	}
	if err := b.Capabilities().ValidateDelete(options); err != nil {
		return err
	}
	input := &awss3.DeleteObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}
	if options.IfMatch != "" {
		input.IfMatch = aws.String(string(options.IfMatch))
	}
	if options.Version != "" {
		input.VersionId = aws.String(string(options.Version))
	}
	_, err = b.client.DeleteObject(ctx, input)
	if err != nil {
		classified := failure(storage.DeleteOperation, storage.Unknown, err)
		if classified.Code() == storage.NotFound {
			if options.IfMatch != "" {
				return storage.Failure(storage.PreconditionFailed, storage.DeleteOperation, storage.Unchanged, err)
			}
			return nil
		}
		return classified
	}
	return failureIf(storage.DeleteOperation, storage.Applied, ctx.Err())
}
