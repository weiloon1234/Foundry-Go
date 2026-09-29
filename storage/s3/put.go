package s3

import (
	"bytes"
	"context"
	"crypto/md5" // S3's Content-MD5 transport integrity, never password/security hashing.
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/storageio"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Put consumes the input exactly once. A declared Size below PartBytes retains
// only that many bytes for a single PutObject; larger or unknown-length inputs
// use pooled PartBytes buffers. PartConcurrency 1 uploads multipart parts
// serially with one buffer; N > 1 keeps up to N parts in flight while one more
// buffer fills. MaxUploads bounds active uploads across disks and queues
// briefly before reporting overload. Only replayable UploadPart calls retry;
// Create, PutObject and Complete never retry ambiguous mutations.
func (b *Backend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	if err := b.ready(ctx, storage.PutOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if source == nil {
		return storage.ObjectInfo{}, storage.Failure(storage.Invalid, storage.PutOperation, storage.Unchanged, nil)
	}
	if err := options.Validate(b.config.MaxObjectBytes); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.Capabilities().ValidatePut(options); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.validateMetadata(options.Metadata); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.admitUpload(ctx); err != nil {
		return storage.ObjectInfo{}, err
	}
	defer b.uploads.Release()
	if options.ContentType == "" {
		options.ContentType = storage.Binary
	}
	writer := b.newWriter(ctx, key, full, options)
	b.trackWrite(key, true)
	defer b.trackWrite(key, false)
	var info storage.ObjectInfo
	err = callback.Invoke("S3 object publication", func() error { var err error; info, err = writer.publish(source); return err })
	// Every part request has exited before the outcome is classified, any
	// abort starts or a pooled buffer is reused.
	if drained := writer.drain(); err != nil && drained != nil && !errors.Is(err, drained) {
		err = errors.Join(err, drained)
	}
	writer.recycle()
	if err == nil {
		return info, nil
	}
	state := storage.Unchanged
	if writer.attempted {
		state = storage.Unknown
	}
	if writer.completed {
		state = storage.Applied
	}
	primary := failure(storage.PutOperation, state, err)
	if writer.upload != "" && !writer.completed {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.config.AbortTimeout)
		cleanup := callback.Invoke("S3 multipart abort", func() error { return b.abort(cleanupContext, full, writer.upload) })
		cancel()
		if cleanup != nil {
			primary = storage.Failure(primary.Code(), storage.PutOperation, primary.Outcome(), errors.Join(primary, cleanup)).WithCleanup(writer.cleanupID())
		}
	}
	if writer.creationUncertain || primary.Outcome() == storage.Unknown && writer.upload != "" {
		primary = primary.WithCleanup(writer.cleanupID())
	}
	return storage.ObjectInfo{}, primary
}

// admitUpload waits in FIFO order for a shared upload slot. Exhaustion is a
// retryable Unavailable failure matching fault.Overloaded; nothing was sent.
func (b *Backend) admitUpload(ctx context.Context) error {
	if err := b.uploads.Acquire(ctx, admission.DefaultWait, b.stop); err != nil {
		if errors.Is(err, fault.Closed) {
			return storage.Failure(storage.Closed, storage.PutOperation, storage.Unchanged, err)
		}
		return storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, err)
	}
	return nil
}

// validateMetadata rejects provider-specific metadata the profile cannot store.
func (b *Backend) validateMetadata(metadata storage.ObjectMetadata) error {
	if metadata.EncryptionKey != "" && b.config.Provider == R2 {
		return storage.Failure(storage.Unsupported, storage.PutOperation, storage.Unchanged, nil)
	}
	return nil
}
func (b *Backend) takeBuffer() []byte {
	buffer := b.buffers.Get().(*[]byte)
	return (*buffer)[:0]
}
func (b *Backend) putBuffer(buffer []byte) {
	buffer = buffer[:0]
	b.buffers.Put(&buffer)
}

type objectFields struct {
	contentType, cacheControl, disposition, encoding, kmsKey *string
	storageClass                                             types.StorageClass
	encryption                                               types.ServerSideEncryption
	metadata                                                 map[string]string
}

// fields maps typed object metadata to one provider request. Custom names
// cannot collide with the reserved foundry- checksum metadata.
func fields(options storage.PutOptions, digest value.Optional[storage.SHA256]) objectFields {
	result := objectFields{contentType: aws.String(string(options.ContentType)), metadata: make(map[string]string, len(options.Metadata.Custom)+1)}
	for name, text := range options.Metadata.Custom {
		result.metadata[name] = text
	}
	if checksum, ok := digest.Get(); ok {
		result.metadata[checksumMetadata] = checksum.String()
	}
	optional := func(text string) *string {
		if text == "" {
			return nil
		}
		return aws.String(text)
	}
	result.cacheControl = optional(options.Metadata.CacheControl)
	result.disposition = optional(options.Metadata.ContentDisposition)
	result.encoding = optional(options.Metadata.ContentEncoding)
	result.storageClass = types.StorageClass(options.Metadata.StorageClass)
	if options.Metadata.EncryptionKey != "" {
		result.encryption = types.ServerSideEncryptionAwsKms
		result.kmsKey = aws.String(options.Metadata.EncryptionKey)
	}
	return result
}

type multipartWriter struct {
	backend *Backend
	// parent is the Put context; ctx additionally ends when a concurrent part
	// fails, so sibling parts stop early.
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	key     storage.ObjectKey
	full    string
	options storage.PutOptions
	buffer  []byte
	pooled  bool
	upload  string
	slots   chan struct{}
	pending sync.WaitGroup
	mu      sync.Mutex
	parts   []types.CompletedPart
	failed  error
	// Publication state is written by the publishing goroutine only.
	attempted, completed, creationUncertain bool
}

func (b *Backend) newWriter(ctx context.Context, key storage.ObjectKey, full string, options storage.PutOptions) *multipartWriter {
	operation, cancel := context.WithCancel(ctx)
	w := &multipartWriter{backend: b, parent: ctx, ctx: operation, cancel: cancel, key: key, full: full, options: options}
	if size, declared := options.Size.Get(); declared && size < b.config.PartBytes {
		// A known small object never needs a full part buffer.
		w.buffer = make([]byte, 0, int(size))
	} else {
		w.buffer, w.pooled = b.takeBuffer(), true
	}
	if b.config.PartConcurrency > 1 {
		w.slots = make(chan struct{}, b.config.PartConcurrency)
	}
	return w
}
func (w *multipartWriter) cleanupID() storage.CleanupID {
	return w.backend.cleanupReference(w.key, w.upload)
}

// wait blocks until every in-flight part exits and returns the first failure.
func (w *multipartWriter) wait() error {
	w.pending.Wait()
	return w.failure()
}

// drain ends the writer after publication returns: no part remains running.
func (w *multipartWriter) drain() error {
	err := w.wait()
	w.cancel()
	return err
}
func (w *multipartWriter) recycle() {
	if w.pooled {
		w.backend.putBuffer(w.buffer)
		w.buffer, w.pooled = nil, false
	}
}
func (w *multipartWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

func (w *multipartWriter) Write(p []byte) (int, error) {
	count := 0
	for len(p) > 0 {
		if len(w.buffer) == cap(w.buffer) {
			if err := w.flush(); err != nil {
				return count, err
			}
		}
		n := min(len(p), cap(w.buffer)-len(w.buffer))
		w.buffer = append(w.buffer, p[:n]...)
		p = p[n:]
		count += n
	}
	return count, nil
}

// flush publishes the filled buffer as the next multipart part, creating the
// upload first when needed.
func (w *multipartWriter) flush() error {
	if w.upload == "" {
		w.creationUncertain = true
		described := fields(w.options, w.options.Checksum)
		out, err := w.backend.client.CreateMultipartUpload(w.ctx, &awss3.CreateMultipartUploadInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), ContentType: described.contentType, Metadata: described.metadata, CacheControl: described.cacheControl, ContentDisposition: described.disposition, ContentEncoding: described.encoding, StorageClass: described.storageClass, ServerSideEncryption: described.encryption, SSEKMSKeyId: described.kmsKey})
		if out != nil {
			w.upload = aws.ToString(out.UploadId)
		}
		if err != nil {
			return err
		}
		if w.upload == "" {
			return storage.IntegrityFailed
		}
		w.creationUncertain = false
	}
	if err := w.failure(); err != nil {
		return err
	}
	w.mu.Lock()
	if len(w.parts) >= MaxParts {
		w.mu.Unlock()
		return storage.LimitExceeded
	}
	number := int32(len(w.parts) + 1)
	w.parts = append(w.parts, types.CompletedPart{})
	w.mu.Unlock()
	if w.slots == nil {
		part, err := w.part(number, w.buffer)
		if err != nil {
			return err
		}
		w.mu.Lock()
		w.parts[number-1] = part
		w.mu.Unlock()
		w.buffer = w.buffer[:0]
		return nil
	}
	select {
	case w.slots <- struct{}{}:
	case <-w.ctx.Done():
		if err := w.failure(); err != nil {
			return err
		}
		return w.ctx.Err()
	}
	body, pooled := w.buffer, w.pooled
	w.buffer, w.pooled = w.backend.takeBuffer(), true
	w.pending.Add(1)
	go func() {
		defer w.pending.Done()
		defer func() { <-w.slots }()
		var part types.CompletedPart
		err := callback.Invoke("S3 multipart part", func() error { var err error; part, err = w.part(number, body); return err })
		if pooled {
			w.backend.putBuffer(body)
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if err != nil {
			if w.failed == nil {
				w.failed = err
			}
			w.cancel()
			return
		}
		w.parts[number-1] = part
	}()
	return nil
}
func (w *multipartWriter) part(number int32, data []byte) (types.CompletedPart, error) {
	digest := md5.Sum(data)
	result, err := w.backend.client.UploadPart(w.ctx, &awss3.UploadPartInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), UploadId: aws.String(w.upload), PartNumber: aws.Int32(number), ContentLength: aws.Int64(int64(len(data))), ContentMD5: aws.String(base64.StdEncoding.EncodeToString(digest[:])), Body: bytes.NewReader(data)}, w.backend.partRetry)
	if err != nil {
		return types.CompletedPart{}, err
	}
	if result == nil || aws.ToString(result.ETag) == "" {
		return types.CompletedPart{}, storage.IntegrityFailed
	}
	if err := storage.ETag(*result.ETag).Validate(); err != nil {
		return types.CompletedPart{}, err
	}
	return types.CompletedPart{ETag: result.ETag, PartNumber: aws.Int32(number)}, nil
}
func (w *multipartWriter) publish(source io.Reader) (storage.ObjectInfo, error) {
	maximum := w.backend.config.MaxObjectBytes
	if expected, ok := w.options.Size.Get(); ok {
		maximum = expected
	}
	size, digest, err := storageio.Copy(w.parent, w, source, maximum)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if expected, ok := w.options.Size.Get(); ok && size != expected {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	if expected, ok := w.options.Checksum.Get(); ok && digest != expected {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	if err := w.parent.Err(); err != nil {
		return storage.ObjectInfo{}, err
	}
	var tag, version string
	var response middleware.Metadata
	if w.upload == "" {
		described := fields(w.options, value.Set(digest))
		md5sum := md5.Sum(w.buffer)
		input := &awss3.PutObjectInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), ContentType: described.contentType, ContentLength: aws.Int64(size), Metadata: described.metadata, Body: bytes.NewReader(w.buffer), ContentMD5: aws.String(base64.StdEncoding.EncodeToString(md5sum[:])), CacheControl: described.cacheControl, ContentDisposition: described.disposition, ContentEncoding: described.encoding, StorageClass: described.storageClass, ServerSideEncryption: described.encryption, SSEKMSKeyId: described.kmsKey}
		if w.options.Condition.RequiresAbsence() {
			input.IfNoneMatch = aws.String("*")
		}
		if match := w.options.Condition.Match(); match != "" {
			input.IfMatch = aws.String(string(match))
		}
		w.attempted = true
		result, err := w.backend.client.PutObject(w.ctx, input)
		if err != nil {
			return storage.ObjectInfo{}, err
		}
		w.completed = true
		if result == nil {
			return storage.ObjectInfo{}, storage.IntegrityFailed
		}
		tag, version, response = aws.ToString(result.ETag), aws.ToString(result.VersionId), result.ResultMetadata
	} else {
		if len(w.buffer) > 0 {
			if err := w.flush(); err != nil {
				return storage.ObjectInfo{}, err
			}
		}
		// Completion lists every part only after all part requests exited.
		if err := w.wait(); err != nil {
			return storage.ObjectInfo{}, err
		}
		w.mu.Lock()
		parts := append([]types.CompletedPart(nil), w.parts...)
		w.mu.Unlock()
		input := &awss3.CompleteMultipartUploadInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), UploadId: aws.String(w.upload), MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}}
		if w.options.Condition.RequiresAbsence() {
			input.IfNoneMatch = aws.String("*")
		}
		if match := w.options.Condition.Match(); match != "" {
			input.IfMatch = aws.String(string(match))
		}
		if w.backend.config.Provider == AWS {
			input.MpuObjectSize = aws.Int64(size)
		}
		w.attempted = true
		result, err := w.backend.client.CompleteMultipartUpload(w.ctx, input)
		if err != nil {
			return storage.ObjectInfo{}, err
		}
		w.completed = true
		if result == nil {
			return storage.ObjectInfo{}, storage.IntegrityFailed
		}
		tag, version, response = aws.ToString(result.ETag), aws.ToString(result.VersionId), result.ResultMetadata
	}
	if tag == "" || storage.ETag(tag).Validate() != nil {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	// The acknowledgement establishes publication. Its response Date stands in
	// for Last-Modified unless an optional read pinned to the acknowledged
	// validator/version returns the provider's own timestamp. A refused or
	// failed verification read (for example write-only credentials) leaves the
	// publication applied and unverified rather than uncertain.
	modified, err := responseTime(response)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	info := storage.ObjectInfo{Key: w.key, Size: size, ContentType: w.options.ContentType, Modified: modified, ETag: storage.ETag(tag), Version: w.backend.versionID(version), Checksum: value.Set(digest)}
	if w.backend.config.VerifyPublication {
		observed, err := w.backend.Stat(w.ctx, w.key, storage.ReadOptions{IfMatch: info.ETag, Version: info.Version})
		if err == nil {
			if observed.Size != size || observed.ContentType != w.options.ContentType {
				return storage.ObjectInfo{}, storage.IntegrityFailed
			}
			info.Modified = observed.Modified
		}
	}
	if err := info.Validate(); err != nil {
		return storage.ObjectInfo{}, storage.Failure(storage.IntegrityFailed, storage.PutOperation, storage.Applied, err)
	}
	return info, nil
}

// responseTime is the provider's response Date, or the local receipt time
// when a compatible provider omits Date.
func responseTime(metadata middleware.Metadata) (temporal.DateTime, error) {
	at, ok := awsmiddleware.GetServerTime(metadata)
	if !ok || at.IsZero() {
		at, ok = awsmiddleware.GetResponseAt(metadata)
	}
	if !ok || at.IsZero() {
		at = time.Now()
	}
	return temporal.NewDateTime(at.UTC())
}
