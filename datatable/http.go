package datatable

import (
	"context"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Download freezes the bounded request, then uses Export when HTTP opens the
// representation. Every open repeats current authorization and query scoping.
// The existing HTTP file transport owns range handling and always closes the
// returned artifact. Subject A must remain valid for that request's lifetime.
//
// Generation and transfer run under the request's single deadline. Declare the
// download route with WithTimeout(m.DownloadTimeout()) so a report is not cut
// by the kernel RequestTimeout; generation is also bounded by ExportTimeout.
// When the request context ends (deadline, client disconnect or shutdown),
// generation stops and releases its export slot, read transaction and
// temporary file. Responses carry the artifact's SHA-256 as a strong ETag plus
// Last-Modified, so a Range resume with If-Range receives the complete new file
// when a later run differs.
func (t Table[S, R, A]) Download(ctx context.Context, m *Manager, subject A, request Request, options ExportOptions) (foundryhttp.Download, error) {
	if err := t.check(m); err != nil {
		return foundryhttp.Download{}, err
	}
	var snapshot Request
	err := m.calls.Run(ctx, "prepare datatable download", func(ctx context.Context) error {
		if _, err := t.definition.prepare(request, m.config); err != nil {
			return err
		}
		encoded, err := RequestJSON().Encode(ctx, request, requestLimits())
		if err != nil {
			return err
		}
		snapshot, err = DecodeRequest(ctx, encoded)
		return err
	})
	if err != nil {
		return foundryhttp.Download{}, err
	}
	return foundryhttp.DownloadFrom(func(ctx context.Context) (foundryhttp.DownloadContent, error) {
		artifact, err := t.Export(ctx, m, subject, snapshot, options)
		if err != nil {
			return foundryhttp.DownloadContent{}, err
		}
		return foundryhttp.DownloadContent{Body: artifact, Name: artifact.Name(), MediaType: artifact.MediaType(), Modified: artifact.Modified(), EntityTag: artifact.EntityTag()}, nil
	}), nil
}

// DownloadTimeout is the route deadline a table download should declare with
// Route or Endpoint WithTimeout: ExportTimeout for generation plus the same
// again for the transfer, at most foundryhttp.MaxRouteTimeout.
func (c Config) DownloadTimeout() time.Duration {
	return min(2*c.ExportTimeout, foundryhttp.MaxRouteTimeout)
}

// DownloadTimeout reports the download route deadline for this manager's
// configuration. See Config.DownloadTimeout.
func (m *Manager) DownloadTimeout() time.Duration {
	if m == nil {
		return 0
	}
	return m.config.DownloadTimeout()
}
