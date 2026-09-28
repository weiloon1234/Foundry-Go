package s3

import (
	"bytes"
	"context"
	"crypto/md5" // S3's Content-MD5 transport integrity, never password/security hashing.
	"encoding/base64"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/storageio"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Put consumes the input exactly once. Up to PartBytes is retained for a single
// PutObject; larger sources use serial multipart parts with that same buffer.
// Parallel objects are bounded by MaxUploads. Only replayable UploadPart calls
// retry. Create, PutObject and Complete never retry ambiguous mutations.
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

	select {
	case b.uploads <- struct{}{}:
		defer func() { <-b.uploads }()
	default:
		return storage.ObjectInfo{}, storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	if options.ContentType == "" {
		options.ContentType = storage.Binary
	}
	writer := &multipartWriter{backend: b, ctx: ctx, key: key, full: full, options: options, buffer: make([]byte, 0, int(b.config.PartBytes))}
	b.trackWrite(key, true)
	defer b.trackWrite(key, false)
	var info storage.ObjectInfo
	err = callback.Isolated("S3 object publication", func() error { var err error; info, err = writer.publish(source); return err })
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
		cleanup := callback.Isolated("S3 multipart abort", func() error { return b.abort(cleanupContext, full, writer.upload) })
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

type multipartWriter struct {
	backend                                 *Backend
	ctx                                     context.Context
	key                                     storage.ObjectKey
	full                                    string
	options                                 storage.PutOptions
	buffer                                  []byte
	upload                                  string
	parts                                   []types.CompletedPart
	attempted, completed, creationUncertain bool
}

func (w *multipartWriter) metadata() map[string]string {
	metadata := make(map[string]string)
	if digest, ok := w.options.Checksum.Get(); ok {
		metadata[checksumMetadata] = digest.String()
	}
	return metadata
}
func (w *multipartWriter) cleanupID() storage.CleanupID {
	return w.backend.cleanupReference(w.key, w.upload)
}

func (w *multipartWriter) Write(p []byte) (int, error) {
	count := 0
	for len(p) > 0 {
		if len(w.buffer) == cap(w.buffer) {
			if w.upload == "" {
				w.creationUncertain = true
				out, err := w.backend.client.CreateMultipartUpload(w.ctx, &awss3.CreateMultipartUploadInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), ContentType: aws.String(string(w.options.ContentType)), Metadata: w.metadata()})
				if out != nil {
					w.upload = aws.ToString(out.UploadId)
				}
				if err != nil {
					return count, err
				}
				if w.upload == "" {
					return count, storage.IntegrityFailed
				}
				w.creationUncertain = false
			}
			if err := w.part(); err != nil {
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
func (w *multipartWriter) part() error {
	if len(w.parts) >= MaxParts {
		return storage.LimitExceeded
	}
	part := int32(len(w.parts) + 1)
	digest := md5.Sum(w.buffer)
	result, err := w.backend.client.UploadPart(w.ctx, &awss3.UploadPartInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), UploadId: aws.String(w.upload), PartNumber: aws.Int32(part), ContentLength: aws.Int64(int64(len(w.buffer))), ContentMD5: aws.String(base64.StdEncoding.EncodeToString(digest[:])), Body: bytes.NewReader(w.buffer)}, safeRetry(w.backend.config.PartAttempts))
	if err != nil {
		return err
	}
	if result == nil || aws.ToString(result.ETag) == "" {
		return storage.IntegrityFailed
	}
	if err := storage.ETag(*result.ETag).Validate(); err != nil {
		return err
	}
	w.parts = append(w.parts, types.CompletedPart{ETag: result.ETag, PartNumber: aws.Int32(part)})
	w.buffer = w.buffer[:0]
	return nil
}
func (w *multipartWriter) publish(source io.Reader) (storage.ObjectInfo, error) {
	maximum := w.backend.config.MaxObjectBytes
	if expected, ok := w.options.Size.Get(); ok {
		maximum = expected
	}
	size, digest, err := storageio.Copy(w.ctx, w, source, maximum)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if expected, ok := w.options.Size.Get(); ok && size != expected {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	if expected, ok := w.options.Checksum.Get(); ok && digest != expected {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	if err := w.ctx.Err(); err != nil {
		return storage.ObjectInfo{}, err
	}
	tag, version := "", ""
	if w.upload == "" {
		metadata := w.metadata()
		metadata[checksumMetadata] = digest.String()
		md5sum := md5.Sum(w.buffer)
		input := &awss3.PutObjectInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), ContentType: aws.String(string(w.options.ContentType)), ContentLength: aws.Int64(size), Metadata: metadata, Body: bytes.NewReader(w.buffer), ContentMD5: aws.String(base64.StdEncoding.EncodeToString(md5sum[:]))}
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
		tag, version = aws.ToString(result.ETag), aws.ToString(result.VersionId)
	} else {
		if len(w.buffer) > 0 {
			if err := w.part(); err != nil {
				return storage.ObjectInfo{}, err
			}
		}
		input := &awss3.CompleteMultipartUploadInput{Bucket: aws.String(w.backend.config.Bucket), Key: aws.String(w.full), UploadId: aws.String(w.upload), MultipartUpload: &types.CompletedMultipartUpload{Parts: w.parts}}
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
		tag, version = aws.ToString(result.ETag), aws.ToString(result.VersionId)
	}
	if tag == "" {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	// The provider owns Modified. Pin this read to the published validator/version
	// instead of fabricating a timestamp or returning another writer's metadata.
	info, err := w.backend.Stat(w.ctx, w.key, storage.ReadOptions{IfMatch: storage.ETag(tag), Version: w.backend.versionID(version)})
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if info.Size != size || info.ContentType != w.options.ContentType {
		return storage.ObjectInfo{}, storage.IntegrityFailed
	}
	info.Checksum = value.Set(digest)
	return info, nil
}
