package s3

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weiloon1234/Foundry-Go/internal/awscredentials"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxSingleUploadBytes is S3's single-request PutObject limit, which bounds a
// presigned upload.
const MaxSingleUploadBytes int64 = 5 << 30

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

// signingLifetime bounds a presigned URL by the request and by known temporary
// credential expiry. A synthetic refresh deadline is not a credential expiry.
func (b *Backend) signingLifetime(ctx context.Context, requested time.Duration) (time.Duration, time.Time, error) {
	if b.credentials == nil {
		return 0, time.Time{}, storage.Failure(storage.Forbidden, storage.SignOperation, storage.NotApplicable, nil)
	}
	credentials, err := b.credentials.Retrieve(ctx)
	if err != nil {
		return 0, time.Time{}, failure(storage.SignOperation, storage.NotApplicable, err)
	}
	now := time.Now().UTC()
	lifetime := requested.Truncate(time.Second)
	if expires, known := awscredentials.KnownExpiry(credentials); known {
		lifetime = min(lifetime, expires.Sub(now).Truncate(time.Second))
	}
	if lifetime < time.Second {
		return 0, time.Time{}, storage.Failure(storage.Forbidden, storage.SignOperation, storage.NotApplicable, nil)
	}
	return lifetime, now, nil
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
	lifetime, now, err := b.signingLifetime(ctx, options.ExpiresIn)
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	input := &awss3.GetObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}
	if options.Version != "" {
		input.VersionId = aws.String(string(options.Version))
	}
	// Response overrides are signed query parameters, not caller headers.
	if options.ResponseContentType != "" {
		input.ResponseContentType = aws.String(string(options.ResponseContentType))
	}
	if options.ResponseContentDisposition != "" {
		input.ResponseContentDisposition = aws.String(options.ResponseContentDisposition)
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

// TemporaryUploadURL presigns one PutObject with its exact length, media type,
// optional checksum metadata and optional absence condition. Every signed
// header except Host is returned for the client to send unchanged.
func (b *Backend) TemporaryUploadURL(ctx context.Context, key storage.ObjectKey, options storage.UploadLinkOptions) (storage.UploadLink, error) {
	if err := b.ready(ctx, storage.SignOperation); err != nil {
		return storage.UploadLink{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return storage.UploadLink{}, err
	}
	if err := options.Validate(min(b.config.MaxObjectBytes, MaxSingleUploadBytes)); err != nil {
		return storage.UploadLink{}, err
	}
	if err := b.Capabilities().ValidatePut(storage.PutOptions{Size: optionalSize(options.Size), Condition: options.Condition}); err != nil {
		return storage.UploadLink{}, err
	}
	lifetime, now, err := b.signingLifetime(ctx, options.ExpiresIn)
	if err != nil {
		return storage.UploadLink{}, err
	}
	input := &awss3.PutObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full), ContentType: aws.String(string(options.ContentType)), ContentLength: aws.Int64(options.Size)}
	if checksum, ok := options.Checksum.Get(); ok {
		input.Metadata = map[string]string{checksumMetadata: checksum.String()}
	}
	if options.Condition.RequiresAbsence() {
		input.IfNoneMatch = aws.String("*")
	}
	request, err := awss3.NewPresignClient(b.client).PresignPutObject(ctx, input, func(o *awss3.PresignOptions) { o.Expires = lifetime })
	if err != nil {
		return storage.UploadLink{}, failure(storage.SignOperation, storage.NotApplicable, err)
	}
	if request == nil || request.Method != http.MethodPut {
		return storage.UploadLink{}, storage.Failure(storage.IntegrityFailed, storage.SignOperation, storage.NotApplicable, nil)
	}
	headers := make(http.Header, len(request.SignedHeader))
	for name, values := range request.SignedHeader {
		// Host and Content-Length are signed but set by the client transport.
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	if headers.Get("Content-Type") != string(options.ContentType) {
		return storage.UploadLink{}, storage.Failure(storage.IntegrityFailed, storage.SignOperation, storage.NotApplicable, nil)
	}
	return storage.NewUploadLink(request.URL, http.MethodPut, headers, now.Add(lifetime))
}
func optionalSize(size int64) value.Optional[int64] { return value.Set(size) }

// TemporaryUploadForm presigns a browser POST-policy upload. The policy pins
// the exact bucket and key, the exact Content-Type, a content-length-range and
// optional checksum metadata; the returned fields include every value the
// policy requires. R2 does not implement POST-object uploads, and a compatible
// profile must declare FormUploads.
func (b *Backend) TemporaryUploadForm(ctx context.Context, key storage.ObjectKey, options storage.UploadFormOptions) (storage.UploadForm, error) {
	if err := b.ready(ctx, storage.SignOperation); err != nil {
		return storage.UploadForm{}, err
	}
	if b.config.Provider == R2 || b.config.Provider == Compatible && !b.config.Compatible.FormUploads {
		return storage.UploadForm{}, storage.Failure(storage.Unsupported, storage.SignOperation, storage.NotApplicable, nil)
	}
	full, err := b.object(key)
	if err != nil {
		return storage.UploadForm{}, err
	}
	if err := options.Validate(min(b.config.MaxObjectBytes, MaxSingleUploadBytes)); err != nil {
		return storage.UploadForm{}, err
	}
	lifetime, now, err := b.signingLifetime(ctx, options.ExpiresIn)
	if err != nil {
		return storage.UploadForm{}, err
	}
	extra := []storage.FormField{{Name: "Content-Type", Value: string(options.ContentType)}}
	conditions := []any{map[string]string{"Content-Type": string(options.ContentType)}, []any{"content-length-range", options.MinSize, options.MaxSize}}
	if checksum, ok := options.Checksum.Get(); ok {
		name := "x-amz-meta-" + checksumMetadata
		extra = append(extra, storage.FormField{Name: name, Value: checksum.String()})
		conditions = append(conditions, map[string]string{name: checksum.String()})
	}
	// The exact key condition is added by the presigner; never starts-with.
	request, err := awss3.NewPresignClient(b.client).PresignPostObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full)}, func(o *awss3.PresignPostOptions) {
		o.Expires = lifetime
		o.Conditions = conditions
	})
	if err != nil {
		return storage.UploadForm{}, failure(storage.SignOperation, storage.NotApplicable, err)
	}
	if request == nil || request.Values["key"] != full || request.Values["policy"] == "" {
		return storage.UploadForm{}, storage.Failure(storage.IntegrityFailed, storage.SignOperation, storage.NotApplicable, nil)
	}
	fields := make([]storage.FormField, 0, len(request.Values)+len(extra))
	for name, text := range request.Values {
		fields = append(fields, storage.FormField{Name: name, Value: text})
	}
	fields = append(fields, extra...)
	slices.SortFunc(fields, func(a, b storage.FormField) int { return strings.Compare(a.Name, b.Name) })
	return storage.NewUploadForm(request.URL, fields, now.Add(lifetime))
}
