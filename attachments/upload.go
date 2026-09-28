package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Upload borrows Source until the operation actually returns and never closes
// it. OriginalName and ContentType are untrusted metadata hints. Media acceptance
// is based on bytes; Image policy performs actual bounded decode/re-encoding.
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
	body, err := io.ReadAll(io.LimitReader(workscope.Reader(ctx, input.Source), policy.MaxBytes+1))
	if err != nil {
		return prepared{}, err
	}
	if int64(len(body)) > policy.MaxBytes {
		return prepared{}, storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	detected, _, err := mime.ParseMediaType(http.DetectContentType(body))
	if err != nil {
		return prepared{}, invalid()
	}
	result := prepared{open: func() io.Reader { return bytes.NewReader(body) }, info: UploadInfo{OriginalName: input.OriginalName, MediaType: storage.MediaType(detected), Size: int64(len(body))}, properties: properties}
	if plan, imageRequired := policy.Image.Get(); imageRequired {
		inspected, err := imaging.Inspect(body, m.image.Limits())
		if err != nil {
			return prepared{}, err
		}
		detected = inspected.Format.MediaType()
		if !accepted(policy, storage.MediaType(detected)) {
			return prepared{}, invalid()
		}
		transformed, err := m.image.ProcessBytes(ctx, body, plan)
		if err != nil {
			return prepared{}, err
		}
		info := transformed.Info()
		result.open = transformed.Reader
		result.info.MediaType = storage.MediaType(info.Format.MediaType())
		result.info.Size = transformed.Size()
		result.info.Width = info.Width
		result.info.Height = info.Height
	} else if !accepted(policy, storage.MediaType(detected)) {
		return prepared{}, invalid()
	}
	if result.info.Size > policy.MaxStoredBytes {
		return prepared{}, storage.Failure(storage.LimitExceeded, storage.PutOperation, storage.Unchanged, nil)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, workscope.Reader(ctx, result.open())); err != nil {
		return prepared{}, err
	}
	copy(result.digest[:], digest.Sum(nil))
	return result, nil
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
