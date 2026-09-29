package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type listCursor struct {
	Scope, Prefix, Token string
	Delimited            bool `json:",omitempty"`
}

func (b *Backend) scope() string {
	hash := sha256.Sum256([]byte(b.config.Endpoint + "\x00" + b.config.Bucket + "\x00" + b.config.Namespace.String()))
	return hex.EncodeToString(hash[:])
}

// List builds entries from one ListObjectsV2 page without per-object requests.
// Listing metadata has no media type or full SHA-256, so those stay zero; use
// Stat for complete metadata. Entries that cannot be framework objects (foreign
// or unparsable keys, oversized objects, missing validators) are skipped and
// counted in Page.Skipped instead of failing the page. Delimited listings map
// CommonPrefixes to Page.Directories.
func (b *Backend) List(ctx context.Context, options storage.ListOptions) (storage.Page, error) {
	if err := b.ready(ctx, storage.ListOperation); err != nil {
		return storage.Page{}, err
	}
	if err := options.Validate(); err != nil {
		return storage.Page{}, err
	}
	if err := b.Capabilities().ValidateList(options); err != nil {
		return storage.Page{}, err
	}
	namespace := b.config.Namespace.String()
	if err := validateKeyText(b.config.requiresNFC(), namespace+options.Prefix.String()); err != nil {
		return storage.Page{}, err
	}
	input := &awss3.ListObjectsV2Input{Bucket: aws.String(b.config.Bucket), Prefix: aws.String(namespace + options.Prefix.String()), MaxKeys: aws.Int32(int32(options.Limit)), EncodingType: types.EncodingTypeUrl}
	if options.Delimited {
		input.Delimiter = aws.String("/")
	}
	if !options.Cursor.IsZero() {
		var cursor listCursor
		if err := decodeCursor(options.Cursor, &cursor); err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		if cursor.Scope != b.scope() || cursor.Prefix != options.Prefix.String() || cursor.Delimited != options.Delimited || cursor.Token == "" {
			return storage.Page{}, storage.Failure(storage.Invalid, storage.ListOperation, storage.NotApplicable, nil)
		}
		input.ContinuationToken = aws.String(cursor.Token)
	}
	result, err := b.client.ListObjectsV2(ctx, input, b.readRetry)
	if err != nil {
		return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
	}
	if result == nil || len(result.Contents)+len(result.CommonPrefixes) > options.Limit || !options.Delimited && len(result.CommonPrefixes) > 0 || result.EncodingType != types.EncodingTypeUrl {
		return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
	}
	page := storage.Page{Objects: make([]storage.ObjectInfo, 0, len(result.Contents))}
	previous := ""
	for _, item := range result.Contents {
		info, ok := b.listed(item, options)
		if !ok || info.Key.String() <= previous {
			page.Skipped++
			continue
		}
		previous = info.Key.String()
		page.Objects = append(page.Objects, info)
	}
	previous = ""
	for _, common := range result.CommonPrefixes {
		full, err := b.decodeListedKey(aws.ToString(common.Prefix))
		relative, inside := strings.CutPrefix(full, namespace)
		if err != nil || !inside || !strings.HasPrefix(relative, options.Prefix.String()) || !strings.HasSuffix(relative, "/") || relative <= previous {
			page.Skipped++
			continue
		}
		directory, err := storage.ParsePrefix(relative)
		if err != nil || strings.Contains(strings.TrimSuffix(strings.TrimPrefix(relative, options.Prefix.String()), "/"), "/") || len(relative) <= len(options.Prefix.String())+1 {
			page.Skipped++
			continue
		}
		previous = relative
		page.Directories = append(page.Directories, directory)
	}
	if aws.ToBool(result.IsTruncated) {
		token := aws.ToString(result.NextContinuationToken)
		if token == "" || token == aws.ToString(input.ContinuationToken) {
			return storage.Page{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		page.Next, err = encodeCursor(listCursor{Scope: b.scope(), Prefix: options.Prefix.String(), Token: token, Delimited: options.Delimited})
		if err != nil {
			return storage.Page{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
	}
	return page, nil
}

// listed converts one listing entry. It reports false for anything that is not
// a readable framework object in this namespace and prefix.
func (b *Backend) listed(item types.Object, options storage.ListOptions) (storage.ObjectInfo, bool) {
	full, err := b.decodeListedKey(aws.ToString(item.Key))
	if err != nil || validateKeyText(b.config.requiresNFC(), full) != nil {
		return storage.ObjectInfo{}, false
	}
	relative, inside := strings.CutPrefix(full, b.config.Namespace.String())
	if !inside {
		return storage.ObjectInfo{}, false
	}
	key, err := storage.ParseKey(relative)
	if err != nil || !options.Prefix.Contains(key) || options.Delimited && strings.Contains(strings.TrimPrefix(relative, options.Prefix.String()), "/") {
		return storage.ObjectInfo{}, false
	}
	if item.Size == nil || item.LastModified == nil || *item.Size < 0 || *item.Size > b.config.MaxObjectBytes {
		return storage.ObjectInfo{}, false
	}
	modified, err := temporal.NewDateTime(*item.LastModified)
	if err != nil {
		return storage.ObjectInfo{}, false
	}
	info := storage.ObjectInfo{Key: key, Size: *item.Size, Modified: modified, ETag: storage.ETag(aws.ToString(item.ETag))}
	if info.ETag == "" || info.ValidateListed() != nil {
		return storage.ObjectInfo{}, false
	}
	return info, true
}
