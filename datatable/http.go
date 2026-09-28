package datatable

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Download freezes the bounded request, then uses Export when HTTP opens the
// representation. Every open repeats current authorization and query scoping.
// The existing HTTP file transport owns range handling and always closes the
// returned artifact. Subject A must remain valid for that request's lifetime.
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
		return foundryhttp.DownloadContent{Body: artifact, Name: artifact.Name(), MediaType: artifact.MediaType()}, nil
	}), nil
}
