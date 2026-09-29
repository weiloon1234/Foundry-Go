package http

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Download defers opening until the typed HTTP handler and authorization succeed.
// It reuses Foundry's download response, including HEAD, conditional requests,
// byte ranges, presentation validation and automatic reader cleanup. Seeking
// streams provider ranges; no complete-object buffer or temporary download file
// is needed. Signed HTTP endpoints can protect local downloads with the existing
// URL signer. Never serve a managed local store with a filesystem file server.
func Download(disk *storage.Disk, key storage.ObjectKey, options storage.ReadOptions) foundryhttp.Download {
	return foundryhttp.DownloadFrom(func(ctx context.Context) (foundryhttp.DownloadContent, error) {
		body, info, err := disk.OpenSeek(ctx, key, options)
		if err != nil {
			return foundryhttp.DownloadContent{}, responseError(err)
		}
		return foundryhttp.DownloadContent{Body: body, MediaType: foundryhttp.MediaType(info.ContentType), Modified: info.Modified.UTC(), EntityTag: foundryhttp.EntityTag(info.ETag)}, nil
	})
}

// Stream works with any storage backend and defers opening to the response phase.
// It describes a finite stream with a known length; HTTP byte-range and validator
// negotiation require Download. WithName controls presentation, not object keys.
func Stream(disk *storage.Disk, key storage.ObjectKey, options storage.ReadOptions) foundryhttp.Stream {
	return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
		body, info, err := disk.Open(ctx, key, options)
		if err != nil {
			return foundryhttp.StreamContent{}, responseError(err)
		}
		return foundryhttp.StreamContent{Body: body, MediaType: foundryhttp.MediaType(info.Object.ContentType), Length: value.Set(info.Length)}, nil
	})
}

// responseError maps storage failures to the shared HTTP envelope. Capacity
// exhaustion (fault.Overloaded, LimitExceeded), disk shutdown and deadlines are
// retryable 503 responses, not 500s; unclassified adapter failures remain 500.
func responseError(err error) error {
	var failure *storage.Error
	if errors.As(err, &failure) {
		switch failure.Code() {
		case storage.NotFound:
			return foundryhttp.NotFound.WithCause(err)
		case storage.Forbidden:
			return foundryhttp.Forbidden.WithCause(err)
		case storage.PreconditionFailed:
			return foundryhttp.PreconditionFailed.WithCause(err)
		case storage.RangeNotSatisfiable:
			return foundryhttp.RangeNotSatisfiable.WithCause(err)
		case storage.LimitExceeded, storage.Closed:
			return foundryhttp.Unavailable.WithCause(err)
		}
	}
	if errorgraph.Is(err, fault.Overloaded) || errorgraph.Is(err, context.DeadlineExceeded) || errorgraph.Is(err, context.Canceled) {
		return foundryhttp.Unavailable.WithCause(err)
	}
	return foundryhttp.InternalError.WithCause(err)
}
