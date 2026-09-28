package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type listCursor struct{ Scope, Prefix, Token string }

func (b *Backend) scope() string {
	hash := sha256.Sum256([]byte(b.config.Endpoint + "\x00" + b.config.Bucket + "\x00" + b.config.Namespace.String()))
	return hex.EncodeToString(hash[:])
}
func (b *Backend) List(ctx context.Context, options storage.ListOptions) (storage.Page, error) {
	if err := b.ready(ctx, storage.ListOperation); err != nil {
		return storage.Page{}, err
	}
	if err := options.Validate(); err != nil {
		return storage.Page{}, err
	}
	if err := validateKeyText(b.config.Provider, b.config.Namespace.String()+options.Prefix.String()); err != nil {
		return storage.Page{}, err
	}
	input := &awss3.ListObjectsV2Input{Bucket: aws.String(b.config.Bucket), Prefix: aws.String(b.config.Namespace.String() + options.Prefix.String()), MaxKeys: aws.Int32(int32(options.Limit)), EncodingType: types.EncodingTypeUrl}
	if !options.Cursor.IsZero() {
		var cursor listCursor
		if err := decodeCursor(options.Cursor, &cursor); err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		if cursor.Scope != b.scope() || cursor.Prefix != options.Prefix.String() || cursor.Token == "" {
			return storage.Page{}, storage.Failure(storage.Invalid, storage.ListOperation, storage.NotApplicable, nil)
		}
		input.ContinuationToken = aws.String(cursor.Token)
	}
	result, err := b.client.ListObjectsV2(ctx, input, safeRetry(b.config.ReadAttempts))
	if err != nil {
		return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
	}
	if result == nil || len(result.Contents) > options.Limit || len(result.CommonPrefixes) > 0 || result.EncodingType != types.EncodingTypeUrl {
		return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
	}
	page := storage.Page{Objects: make([]storage.ObjectInfo, 0, len(result.Contents))}
	previous := ""
	for _, item := range result.Contents {
		full, err := b.decodeListedKey(aws.ToString(item.Key))
		if err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		if !strings.HasPrefix(full, b.config.Namespace.String()) {
			return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		key, err := storage.ParseKey(strings.TrimPrefix(full, b.config.Namespace.String()))
		if err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		if !options.Prefix.Contains(key) || key.String() <= previous || aws.ToString(item.ETag) == "" {
			return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		previous = key.String()
		// ListObjects has no media type/full SHA metadata. Pin bounded HEAD calls to
		// its validators; skip objects concurrently deleted or replaced. The page
		// cursor still advances over the provider's listing, not a snapshot promise.
		info, err := b.Stat(ctx, key, storage.ReadOptions{IfMatch: storage.ETag(aws.ToString(item.ETag))})
		if err != nil {
			var failed *storage.Error
			if errors.As(err, &failed) && (failed.Code() == storage.NotFound || failed.Code() == storage.PreconditionFailed) {
				continue
			}
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		page.Objects = append(page.Objects, info)
	}
	if aws.ToBool(result.IsTruncated) {
		token := aws.ToString(result.NextContinuationToken)
		if token == "" || token == aws.ToString(input.ContinuationToken) {
			return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		page.Next, err = encodeCursor(listCursor{Scope: b.scope(), Prefix: options.Prefix.String(), Token: token})
		if err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
	}
	return page, nil
}
