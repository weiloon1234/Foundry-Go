package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/mediatype"
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
	open       func() io.Reader
	info       UploadInfo
	digest     storage.SHA256
	properties value.JSON[json.RawMessage]
}

func prepare(ctx context.Context, m *Manager, policy Policy, input Upload) (prepared, error) {
	input.OriginalName = filename.StripInvisible(input.OriginalName)
	if input.Source == nil || !validFilename(input.OriginalName) {
		return prepared{}, invalid()
	}
	if input.ContentType != "" {
		if err := input.ContentType.Validate(); err != nil {
			return prepared{}, err
		}
	}
	properties, err := normalizeProperties(input.Properties)
	if err != nil {
		return prepared{}, err
	}
	// The stored-bytes digest is computed in the same pass that reads the
	// source; an image policy rehashes only its re-encoded output.
	digest := sha256.New()
	body, err := io.ReadAll(io.TeeReader(io.LimitReader(workscope.Reader(ctx, input.Source), policy.MaxBytes+1), digest))
	if err != nil {
		return prepared{}, err
	}
	if int64(len(body)) > policy.MaxBytes {
		return prepared{}, storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	detected, err := acceptMedia(m, policy, body, input.ContentType)
	if err != nil {
		return prepared{}, err
	}
	result := prepared{open: func() io.Reader { return bytes.NewReader(body) }, info: UploadInfo{OriginalName: input.OriginalName, MediaType: detected, Size: int64(len(body))}, properties: properties}
	if plan, imageRequired := policy.Image.Get(); imageRequired {
		transformed, err := m.image.ProcessBytes(ctx, body, plan)
		if err != nil {
			return prepared{}, err
		}
		info := transformed.Info()
		result.open = transformed.Reader
		digest.Reset()
		if _, err := io.Copy(digest, transformed.Reader()); err != nil {
			return prepared{}, err
		}
		result.info.MediaType = storage.MediaType(info.Format.MediaType())
		result.info.Size = transformed.Size()
		result.info.Width = info.Width
		result.info.Height = info.Height
	}
	if result.info.Size > policy.MaxStoredBytes {
		return prepared{}, storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	copy(result.digest[:], digest.Sum(nil))
	return result, nil
}

// acceptMedia detects media from bytes and applies the policy's acceptance,
// inspecting image input under an image plan. Uploads and slot Accepts share
// it, so request validation and writes agree. A client hint may specialize
// generic text (for example to text/csv) only when the policy accepts that
// specialization; otherwise the byte-detected type stands.
func acceptMedia(m *Manager, policy Policy, body []byte, hint storage.MediaType) (storage.MediaType, error) {
	detected := storage.MediaType(mediatype.Detect(body, ""))
	if specialized := storage.MediaType(mediatype.Detect(body, string(hint))); specialized != detected && accepted(policy, specialized) {
		detected = specialized
	}
	if policy.Image.IsSet() {
		inspected, err := imaging.Inspect(body, m.image.Limits())
		if err != nil {
			return "", err
		}
		if !accepted(policy, storage.MediaType(inspected.Format.MediaType())) {
			return "", invalid()
		}
		return detected, nil
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
