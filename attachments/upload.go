package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/mediatype"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Upload borrows Source until the operation actually returns and never closes
// it. OriginalName and ContentType are untrusted metadata hints. Media acceptance
// is based on bytes; ContentType can only specialize generic plain text to a
// compatible textual type (CSV, TSV, Markdown, calendar, valid JSON) that the
// policy accepts, otherwise the file stays text/plain. Image
// policy performs actual bounded decode/re-encoding. Invisible bidirectional
// and zero-width characters are removed from OriginalName.
type Upload struct {
	Source       io.Reader
	OriginalName string
	ContentType  storage.MediaType
	Properties   value.JSON[json.RawMessage]
}
type prepared struct {
	body       preparedBody
	info       UploadInfo
	digest     storage.SHA256
	properties value.JSON[json.RawMessage]
}

type preparedBody interface {
	open(context.Context) (io.ReadCloser, error)
	close() error
}
type memoryBody struct{ reader func() io.Reader }

func (b memoryBody) open(context.Context) (io.ReadCloser, error) {
	return io.NopCloser(b.reader()), nil
}
func (memoryBody) close() error { return nil }

type capturedBody struct {
	batch *upload.Batch
	file  upload.File
}

func (b capturedBody) open(ctx context.Context) (io.ReadCloser, error) { return b.file.Open(ctx) }
func (b capturedBody) close() error                                    { return b.batch.Close() }

func prepare(ctx context.Context, m *Manager, policy Policy, input Upload) (prepared, error) {
	return prepareInput(ctx, m, policy, input, true)
}
func inputMaximum(policy Policy) int64 {
	if policy.Image.IsSet() {
		return policy.MaxBytes
	}
	return min(policy.MaxBytes, policy.MaxStoredBytes)
}
func prepareInput(ctx context.Context, m *Manager, policy Policy, input Upload, transform bool) (prepared, error) {
	if input.Source == nil {
		return prepared{}, invalid()
	}
	input, err := prepareMetadata(input)
	if err != nil {
		return prepared{}, err
	}
	if policy.MaxBytes > MaxBufferedBytes || policy.MaxStoredBytes > MaxBufferedBytes {
		return prepareCaptured(ctx, m, policy, input, transform, func(batch *upload.Batch) (upload.File, error) {
			return batch.Capture(ctx, input.Source, input.OriginalName, string(input.ContentType))
		})
	}
	// The stored-bytes digest is computed in the same pass that reads the
	// source; an image policy rehashes only its re-encoded output.
	digest := sha256.New()
	body, err := io.ReadAll(io.TeeReader(io.LimitReader(workscope.Reader(ctx, input.Source), inputMaximum(policy)+1), digest))
	if err != nil {
		return prepared{}, safeerror.Owned("uploaded bytes could not be read", &upload.ReadError{Cause: err})
	}
	var checksum storage.SHA256
	copy(checksum[:], digest.Sum(nil))
	return prepareBytes(ctx, m, policy, input, body, checksum, transform)
}

// prepareCaptured transfers its batch only after complete validation. Until then,
// every failure (including a producer panic or Goexit) removes the partial spool.
func prepareCaptured(ctx context.Context, m *Manager, policy Policy, input Upload, transform bool, capture func(*upload.Batch) (upload.File, error)) (result prepared, err error) {
	maximum := inputMaximum(policy)
	batch, err := upload.New(ctx, upload.Config{TempDirectory: m.config.TempDirectory, MaxBytes: maximum, MaxFileBytes: maximum, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		return prepared{}, err
	}
	owned := true
	defer func() {
		if owned {
			if cleanup := batch.Close(); cleanup != nil {
				err = joinFailures(err, safeerror.Owned("upload cleanup failed", &upload.CleanupError{Cause: cleanup}))
			}
		}
	}()
	file, err := capture(batch)
	if err != nil {
		if _, limit := safeerror.UnwrapOwned(err).(*upload.LimitError); limit {
			err = storageFailure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, err)
		}
		return prepared{}, err
	}
	checksum := storage.SHA256(file.SHA256())
	if file.Size() <= MaxBufferedBytes {
		// Exact allocation avoids ReadAll growth near the in-memory ceiling.
		reader, err := file.Open(ctx)
		if err != nil {
			return prepared{}, err
		}
		body := make([]byte, int(file.Size()))
		_, readErr := io.ReadFull(reader, body)
		if err := joinFailures(readErr, reader.Close()); err != nil {
			return prepared{}, err
		}
		input.ContentType = hintOf(file)
		result, err = prepareBytes(ctx, m, policy, input, body, checksum, transform)
		if err != nil {
			return prepared{}, err
		}
		if policy.Image.IsSet() && transform {
			return result, nil
		}
	} else {
		media, _, err := mime.ParseMediaType(file.ContentType())
		if err != nil || !accepted(policy, storage.MediaType(media)) {
			return prepared{}, invalid()
		}
		result = prepared{info: UploadInfo{OriginalName: input.OriginalName, MediaType: storage.MediaType(media), Size: file.Size()}, properties: input.Properties, digest: checksum}
	}
	result.body = capturedBody{batch: batch, file: file}
	owned = false
	return result, nil
}

// prepareMetadata validates upload hints before reading source bytes.
func prepareMetadata(input Upload) (Upload, error) {
	input.OriginalName = filename.StripInvisible(input.OriginalName)
	if !validFilename(input.OriginalName) {
		return Upload{}, invalid()
	}
	if input.ContentType != "" {
		if err := input.ContentType.Validate(); err != nil {
			return Upload{}, err
		}
	}
	properties, err := normalizeProperties(input.Properties)
	if err != nil {
		return Upload{}, err
	}
	input.Properties = properties
	return input, nil
}

// prepareBytes borrows already owned bytes through validation and storage. Both
// ordinary uploads and downloads use the same acceptance and image policy.
func prepareBytes(ctx context.Context, m *Manager, policy Policy, input Upload, body []byte, checksum storage.SHA256, transform bool) (prepared, error) {
	if int64(len(body)) > policy.MaxBytes {
		return prepared{}, storageFailure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	detected, err := acceptMedia(ctx, m, policy, body, input.ContentType)
	if err != nil {
		return prepared{}, err
	}
	result := prepared{body: memoryBody{reader: func() io.Reader { return bytes.NewReader(body) }}, info: UploadInfo{OriginalName: input.OriginalName, MediaType: detected, Size: int64(len(body))}, properties: input.Properties, digest: checksum}
	if plan, imageRequired := policy.Image.Get(); imageRequired && transform {
		transformed, err := m.image.ProcessBytes(ctx, body, plan)
		if err != nil {
			return prepared{}, err
		}
		info := transformed.Info()
		result.body = memoryBody{reader: transformed.Reader}
		digest := sha256.New()
		if _, err := io.Copy(digest, transformed.Reader()); err != nil {
			return prepared{}, err
		}
		copy(result.digest[:], digest.Sum(nil))
		result.info.MediaType = storage.MediaType(info.Format.MediaType())
		result.info.Size = transformed.Size()
		result.info.Width = info.Width
		result.info.Height = info.Height
	}
	if (transform || !policy.Image.IsSet()) && result.info.Size > policy.MaxStoredBytes {
		return prepared{}, storageFailure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	return result, nil
}

// acceptMedia detects media from bytes and applies the policy's acceptance,
// inspecting image input under an image plan. Uploads and slot Accepts share
// it, so request validation and writes agree. A client hint may specialize
// generic text (for example to text/csv) only when the policy accepts that
// specialization; otherwise the byte-detected type stands.
func acceptMedia(ctx context.Context, m *Manager, policy Policy, body []byte, hint storage.MediaType) (storage.MediaType, error) {
	detected := storage.MediaType(mediatype.Detect(body, ""))
	if specialized := storage.MediaType(mediatype.Detect(body, string(hint))); specialized != detected && accepted(policy, specialized) {
		detected = specialized
	}
	if policy.Image.IsSet() || len(policy.Variants) > 0 {
		inspected, err := m.image.Inspect(ctx, body)
		if err != nil {
			return "", err
		}
		if !accepted(policy, storage.MediaType(inspected.Format.MediaType())) {
			return "", invalid()
		}
		return storage.MediaType(inspected.Format.MediaType()), nil
	}
	if !accepted(policy, detected) {
		return "", invalid()
	}
	return detected, nil
}

func accepted(policy Policy, media storage.MediaType) bool {
	if policy.AnyMedia || policy.Image.IsSet() && len(policy.Accepted) == 0 {
		return true
	}
	for _, allowed := range policy.Accepted {
		if allowed == media {
			return true
		}
		if policy.Image.IsSet() || len(policy.Variants) > 0 {
			expected, e1 := imaging.ParseMediaType(string(allowed))
			actual, e2 := imaging.ParseMediaType(string(media))
			if e1 == nil && e2 == nil && expected == actual {
				return true
			}
		}
	}
	return false
}
func normalizeProperties(input value.JSON[json.RawMessage]) (value.JSON[json.RawMessage], error) {
	if input.IsZero() {
		return value.ParseJSON[json.RawMessage](`{}`)
	}
	text, err := input.Text()
	if err != nil {
		return value.JSON[json.RawMessage]{}, err
	}
	if len(text) > MaxPropertiesBytes || len(text) == 0 || text[0] != '{' {
		return value.JSON[json.RawMessage]{}, invalid()
	}
	return input, nil
}

func validFilename(name string) bool {
	if len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
