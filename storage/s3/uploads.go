package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// UploadReference identifies one staging upload in this adapter's namespace.
// It grants no authorization. Obtain it from ListUploads or UploadFromCleanup;
// it cannot accidentally be passed as an object/version key to Delete.
type UploadReference struct {
	scope  string
	key    storage.ObjectKey
	upload string
}

func (UploadReference) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("storage multipart reference"))
}
func (UploadReference) MarshalJSON() ([]byte, error) { return nil, storage.Invalid }
func (r UploadReference) Key() storage.ObjectKey     { return r.key }

type PendingUpload struct {
	Reference UploadReference
	Initiated temporal.DateTime
}
type UploadPage struct {
	Uploads []PendingUpload
	Next    storage.Cursor
}
type uploadCursor struct{ Scope, Prefix, Key, Upload string }
type cleanupReference struct{ Scope, Key, Upload string }

func (b *Backend) cleanupReference(key storage.ObjectKey, upload string) storage.CleanupID {
	data, _ := json.Marshal(cleanupReference{b.scope(), key.String(), upload})
	return storage.NewCleanupID(string(data))
}

// UploadFromCleanup validates a reference against this exact endpoint, bucket
// and namespace. A lost creation response without an upload ID returns
// Unsupported: inspect ListUploads and establish ownership before aborting.
func (b *Backend) UploadFromCleanup(id storage.CleanupID) (UploadReference, error) {
	if b == nil || !b.prepared {
		return UploadReference{}, storage.Failure(storage.Invalid, storage.DeleteOperation, storage.Unchanged, nil)
	}
	if len(id.Value()) > storage.MaxCursorBytes {
		return UploadReference{}, storage.Invalid
	}
	var ref cleanupReference
	decoder := json.NewDecoder(strings.NewReader(id.Value()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ref) != nil {
		return UploadReference{}, storage.Invalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || ref.Scope != b.scope() {
		return UploadReference{}, storage.Invalid
	}
	key, err := storage.ParseKey(ref.Key)
	if err != nil {
		return UploadReference{}, err
	}
	if ref.Upload == "" {
		return UploadReference{}, storage.Unsupported
	}
	if !validUploadID(ref.Upload) {
		return UploadReference{}, storage.Invalid
	}
	return UploadReference{b.scope(), key, ref.Upload}, nil
}
func validUploadID(id string) bool {
	if len(id) == 0 || len(id) > 4096 {
		return false
	}
	for _, ch := range id {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	return true
}

// ListUploads only inspects bounded staging pages. Age alone does not prove an
// upload is abandoned. Its owner must be stopped or otherwise reconciled before
// AbortUpload is called; this API never automatically prunes by age.
func (b *Backend) ListUploads(ctx context.Context, options storage.ListOptions) (UploadPage, error) {
	if err := b.ready(ctx, storage.ListOperation); err != nil {
		return UploadPage{}, err
	}
	if err := options.Validate(); err != nil {
		return UploadPage{}, err
	}
	if err := validateKeyText(b.config.requiresNFC(), b.config.Namespace.String()+options.Prefix.String()); err != nil {
		return UploadPage{}, err
	}
	input := &awss3.ListMultipartUploadsInput{Bucket: aws.String(b.config.Bucket), Prefix: aws.String(b.config.Namespace.String() + options.Prefix.String()), MaxUploads: aws.Int32(int32(options.Limit)), EncodingType: types.EncodingTypeUrl}
	if !options.Cursor.IsZero() {
		var cursor uploadCursor
		if err := decodeCursor(options.Cursor, &cursor); err != nil {
			return UploadPage{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		if cursor.Scope != b.scope() || cursor.Prefix != options.Prefix.String() || !validUploadID(cursor.Upload) || !strings.HasPrefix(cursor.Key, aws.ToString(input.Prefix)) {
			return UploadPage{}, storage.Failure(storage.Invalid, storage.ListOperation, storage.NotApplicable, nil)
		}
		if _, err := storage.ParseKey(cursor.Key); err != nil {
			return UploadPage{}, err
		}
		if err := validateKeyText(b.config.requiresNFC(), cursor.Key); err != nil {
			return UploadPage{}, err
		}
		input.KeyMarker = aws.String(cursor.Key)
		input.UploadIdMarker = aws.String(cursor.Upload)
	}
	result, err := b.client.ListMultipartUploads(ctx, input, b.readRetry)
	if err != nil {
		return UploadPage{}, failure(storage.ListOperation, storage.NotApplicable, err)
	}
	if result == nil || len(result.Uploads) > options.Limit || len(result.CommonPrefixes) > 0 || result.EncodingType != types.EncodingTypeUrl {
		return UploadPage{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
	}
	page := UploadPage{Uploads: make([]PendingUpload, 0, len(result.Uploads))}
	seen := make(map[string]bool, len(result.Uploads))
	for _, entry := range result.Uploads {
		full, err := b.decodeListedKey(aws.ToString(entry.Key))
		if err != nil || !strings.HasPrefix(full, aws.ToString(input.Prefix)) {
			return UploadPage{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, err)
		}
		if err := validateKeyText(b.config.requiresNFC(), full); err != nil {
			return UploadPage{}, err
		}
		key, err := storage.ParseKey(strings.TrimPrefix(full, b.config.Namespace.String()))
		if err != nil {
			return UploadPage{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
		upload := aws.ToString(entry.UploadId)
		if !validUploadID(upload) || seen[upload] || entry.Initiated == nil {
			return UploadPage{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, nil)
		}
		seen[upload] = true
		initiated, err := temporal.NewDateTime(*entry.Initiated)
		if err != nil || initiated.IsZero() {
			return UploadPage{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, err)
		}
		page.Uploads = append(page.Uploads, PendingUpload{Reference: UploadReference{b.scope(), key, upload}, Initiated: initiated})
	}
	if aws.ToBool(result.IsTruncated) {
		next, err := b.decodeListedKey(aws.ToString(result.NextKeyMarker))
		upload := aws.ToString(result.NextUploadIdMarker)
		if err != nil || !validUploadID(upload) || !strings.HasPrefix(next, aws.ToString(input.Prefix)) || next == aws.ToString(input.KeyMarker) && upload == aws.ToString(input.UploadIdMarker) {
			return UploadPage{}, storage.Failure(storage.IntegrityFailed, storage.ListOperation, storage.NotApplicable, err)
		}
		page.Next, err = encodeCursor(uploadCursor{b.scope(), options.Prefix.String(), next, upload})
		if err != nil {
			return UploadPage{}, failure(storage.ListOperation, storage.NotApplicable, err)
		}
	}
	return page, nil
}

// AbortUpload affects only the referenced staging upload, never a published
// object. Keys being written by this adapter are rejected, even before an upload ID is known. Other-process owners
// must be stopped by the operator first; storage age is not a lease.
func (b *Backend) AbortUpload(ctx context.Context, ref UploadReference) error {
	if err := b.ready(ctx, storage.DeleteOperation); err != nil {
		return err
	}
	if ref.scope != b.scope() || !validUploadID(ref.upload) {
		return storage.Failure(storage.Invalid, storage.DeleteOperation, storage.Unchanged, nil)
	}
	full, err := b.object(ref.key)
	if err != nil {
		return err
	}
	b.activeMu.Lock()
	_, active := b.activeWrites[ref.key]
	b.activeMu.Unlock()
	if active {
		return storage.Failure(storage.PreconditionFailed, storage.DeleteOperation, storage.Unchanged, nil)
	}
	if err := b.abort(ctx, full, ref.upload); err != nil {
		return failure(storage.DeleteOperation, storage.Unknown, err).WithCleanup(b.cleanupReference(ref.key, ref.upload))
	}
	return nil
}
func (b *Backend) trackWrite(key storage.ObjectKey, active bool) {
	b.activeMu.Lock()
	defer b.activeMu.Unlock()
	if active {
		b.activeWrites[key]++
	} else {
		b.activeWrites[key]--
		if b.activeWrites[key] == 0 {
			delete(b.activeWrites, key)
		}
	}
}

// A canceled UploadPart may still finish at the provider after its client call
// returns. Abort then inspect remaining parts; repeat a bounded number of rounds.
// Absence/empty parts prove cleanup only after the owner stops issuing new parts.
func (b *Backend) abort(ctx context.Context, full, upload string) error {
	for round := 0; round < 3; round++ {
		_, err := b.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full), UploadId: aws.String(upload)}, b.partRetry)
		if noSuchUpload(err) {
			return nil
		}
		if err != nil {
			return err
		}
		remaining, err := b.client.ListParts(ctx, &awss3.ListPartsInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(full), UploadId: aws.String(upload), MaxParts: aws.Int32(1)}, b.readRetry)
		if noSuchUpload(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if remaining == nil {
			return storage.IntegrityFailed
		}
		if len(remaining.Parts) == 0 && !aws.ToBool(remaining.IsTruncated) {
			return nil
		}
	}
	return storage.Failure(storage.Unavailable, storage.DeleteOperation, storage.Unknown, nil)
}
