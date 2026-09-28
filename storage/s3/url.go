package s3

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func (b *Backend) PublicURL(ctx context.Context, key storage.ObjectKey) (string, error) {
	if err := b.ready(ctx, storage.SignOperation); err != nil {
		return "", err
	}
	full, err := b.object(key)
	if err != nil {
		return "", err
	}
	if b.config.PublicBase.IsZero() {
		return "", storage.Failure(storage.Unsupported, storage.SignOperation, storage.NotApplicable, nil)
	}
	scoped, err := storage.ParseKey(full)
	if err != nil {
		return "", err
	}
	return b.config.PublicBase.URL(scoped)
}
func (b *Backend) TemporaryURL(ctx context.Context, key storage.ObjectKey, options storage.LinkOptions) (storage.TemporaryURL, error) {
	if err := b.ready(ctx, storage.SignOperation); err != nil {
		return storage.TemporaryURL{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	if err := options.Validate(); err != nil {
		return storage.TemporaryURL{}, err
	}
	if options.Version == "null" || options.Version != "" && !b.Capabilities().Versions {
		return storage.TemporaryURL{}, storage.Failure(storage.Unsupported, storage.SignOperation, storage.NotApplicable, nil)
	}
	if b.credentials == nil {
		return storage.TemporaryURL{}, storage.Failure(storage.Forbidden, storage.SignOperation, storage.NotApplicable, nil)
	}
	credentials, err := b.credentials.Retrieve(ctx)
	if err != nil {
		return storage.TemporaryURL{}, failure(storage.SignOperation, storage.NotApplicable, err)
	}
	now := time.Now().UTC()
	lifetime := options.ExpiresIn.Truncate(time.Second)
	if credentials.CanExpire {
		lifetime = min(lifetime, credentials.Expires.Sub(now).Truncate(time.Second))
	}
	if lifetime < time.Second {
		return storage.TemporaryURL{}, storage.Failure(storage.Forbidden, storage.SignOperation, storage.NotApplicable, nil)
	}
	input := &awss3.GetObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}
	if options.Version != "" {
		input.VersionId = aws.String(string(options.Version))
	}
	request, err := awss3.NewPresignClient(b.client).PresignGetObject(ctx, input, func(o *awss3.PresignOptions) { o.Expires = lifetime })
	if err != nil {
		return storage.TemporaryURL{}, failure(storage.SignOperation, storage.NotApplicable, err)
	}
	if request == nil || request.Method != "GET" {
		return storage.TemporaryURL{}, storage.Failure(storage.IntegrityFailed, storage.SignOperation, storage.NotApplicable, nil)
	}
	// A bare URL must not hide additional mandatory caller-supplied headers.
	for name := range request.SignedHeader {
		if !strings.EqualFold(name, "Host") {
			return storage.TemporaryURL{}, storage.Failure(storage.Unsupported, storage.SignOperation, storage.NotApplicable, nil)
		}
	}
	return storage.NewTemporaryURL(request.URL, now.Add(lifetime))
}
