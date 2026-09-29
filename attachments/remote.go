package attachments

import (
	"bytes"
	"context"
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
// fail without adding a file. The download completes before manager admission
// and holds no upload slot.
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
	var body bytes.Buffer
	downloaded, err := source.Client.Download(ctx, request, &body, c.definition.policy.MaxBytes)
	if err != nil {
		return Result[M, K]{}, err
	}
	name := source.OriginalName
	if name == "" {
		name = remoteName(source.URL)
	}
	var hint storage.MediaType
	if media, _, err := mime.ParseMediaType(downloaded.Headers.Get("Content-Type")); err == nil && storage.MediaType(media).Validate() == nil {
		hint = storage.MediaType(media)
	}
	return c.Add(ctx, m, owner, Upload{Source: bytes.NewReader(body.Bytes()), OriginalName: name, ContentType: hint})
}

// remoteName derives a display name from the URL's last path segment.
func remoteName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return filename.Normalize("", "download")
	}
	return filename.Normalize(path.Base(parsed.Path), "download")
}
