package httpuploads

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// VideoInput is a one-file multipart transport contract.
type VideoInput struct{ File foundryhttp.UploadedFile }

// VideoService retains domain publication and transaction ownership.
type VideoService interface {
	Store(context.Context, foundryhttp.UploadedFile) error
}

// doc-example:begin admitted-upload
// LargeRouter validates the request directory without claiming it.
// The framework server owns the claim and close. An application shutdown hook
// may also close the returned pool after HTTP drain.
func LargeRouter(settings foundryhttp.UploadSettings, service VideoService) (*foundryhttp.Router, *foundryhttp.UploadPool, error) {
	if err := settings.Validate(); err != nil {
		return nil, nil, err
	}
	pool, err := foundryhttp.NewUploadPool(settings.Request)
	if err != nil {
		return nil, nil, err
	}
	form := foundryhttp.MultipartAdmission(foundryhttp.DefineMultipart(
		foundryhttp.FilePart("video", func(input *VideoInput) *foundryhttp.UploadedFile { return &input.File }),
	), pool)
	limits := foundryhttp.DefaultEndpointLimits()
	limits.Multipart.Bytes, limits.Multipart.FileBytes = settings.BodyBytes, settings.FileBytes
	limits.Multipart.Files, limits.Multipart.Parts, limits.Multipart.Readers = 1, 1, 1
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "videos.store", Method: foundryhttp.POST, Access: foundryhttp.Public},
		foundryhttp.StaticPath("/attachments/video"), foundryhttp.BodyLimit(settings.BodyBytes), foundryhttp.Timeout(settings.Timeout))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form),
		foundryhttp.EmptyResponse(204), foundryhttp.Limits(limits))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, VideoInput]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, service.Store(ctx, input.Body.File)
	}))
	if err != nil {
		_ = pool.Close()
		return nil, nil, err
	}
	return router, pool, nil
}

// doc-example:end admitted-upload
