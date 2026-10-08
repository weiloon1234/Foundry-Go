package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"mime"
	"net/url"
	"path"

	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// RemoteSource names a remote file to import. URL is untrusted input, so
// Client must enforce a restricted destination policy (for example
// httpclient.PublicDestinations) that keeps imports away from internal
// services. OriginalName defaults to the URL's last path segment.
type RemoteSource struct {
	Client       *httpclient.Client
	URL          string
	OriginalName string
}

// AddFromURL downloads one remote file within the collection's MaxBytes and
// adds it exactly like Add. Acceptance is detected from the downloaded bytes;
// the response Content-Type is only a hint. Redirects and non-2xx responses
// fail without adding a file. Manager admission covers the complete download,
// validation, storage and publication, retaining one bounded input buffer. Image
// processing remains subject to the image engine's separate limits.
func (c Collection[M, K]) AddFromURL(ctx context.Context, m *Manager, owner model.Reference[M, K], source RemoteSource) (Result[M, K], error) {
	if err := c.check(m); err != nil {
		return Result[M, K]{}, err
	}
	if !source.Client.RestrictsDestinations() {
		return Result[M, K]{}, invalid()
	}
	request := source.Client.Get(source.URL)
	if err := request.Validate(); err != nil {
		return Result[M, K]{}, err
	}
	name := source.OriginalName
	if name == "" {
		name = remoteName(source.URL)
	}
	var result Result[M, K]
	err := m.calls.Run(ctx, "attachment remote upload", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		input, err := prepareMetadata(Upload{OriginalName: name})
		if err != nil {
			return err
		}
		var body bytes.Buffer
		digest := sha256.New()
		downloaded, err := source.Client.Download(ctx, request, io.MultiWriter(&body, digest), c.definition.policy.MaxBytes)
		if err != nil {
			return err
		}
		if media, _, err := mime.ParseMediaType(downloaded.Headers.Get("Content-Type")); err == nil && storage.MediaType(media).Validate() == nil {
			input.ContentType = storage.MediaType(media)
		}
		var checksum storage.SHA256
		copy(checksum[:], digest.Sum(nil))
		candidate, err := prepareBytes(ctx, m, c.definition.policy, input, body.Bytes(), checksum)
		if err != nil {
			return err
		}
		return c.writePrepared(ctx, m, owner, locale, candidate, false, &result)
	})
	return result, err
}

// remoteName derives a display name from the URL's last path segment.
func remoteName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return filename.Normalize("", "download")
	}
	return filename.Normalize(path.Base(parsed.Path), "download")
}
