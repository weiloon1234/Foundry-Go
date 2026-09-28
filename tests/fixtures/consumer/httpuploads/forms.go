// Package httpuploads exercises generated multipart input from an independent
// consumer. Domain services receive concrete values and request-owned files.
package httpuploads

import (
	"context"
	"io"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Gallery []foundryhttp.UploadedFile

type Settings struct {
	Caption string                                 `json:"caption"`
	Note    value.Optional[value.Nullable[string]] `json:"note,omitzero"`
}

//foundry:multipart
type ProfileInput struct {
	Title      value.Optional[string]
	Attachment foundryhttp.UploadedFile `form:"document"`
	Photos     Gallery
	Tags       []string                 `form:"tags[]"`
	Settings   value.Optional[Settings] `form:",json"`
}

//foundry:dto
type UploadReply struct {
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	DetectedType string `json:"detected_type"`
}

// Service owns the domain operation. Transport decoding, file validation and
// temporary-file cleanup stay inside Foundry's endpoint lifecycle.
type Service interface {
	Save(context.Context, ProfileInput) (UploadReply, error)
}

func Router(directory string, service Service) (*foundryhttp.Router, error) {
	form := ProfileInputDescriptor().WithTempDirectory(directory)
	fields := ProfileInputValidationFields()
	rules := validation.All(
		fields.Title.Rules(validation.Optional(validation.MaxLength[string](80))),
		fields.Attachment.Rules(
			validation.FileMinSize[foundryhttp.UploadedFile](1),
			validation.FileMaxSize[foundryhttp.UploadedFile](1<<20),
			validation.FileContentTypes[foundryhttp.UploadedFile]("text/plain", "image/png"),
			validation.FileExtensions[foundryhttp.UploadedFile]("txt", "png"),
		),
		fields.Photos.Rules(validation.Each[Gallery](validation.FileMaxSize[foundryhttp.UploadedFile](1<<20))),
	)
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "uploads.profile", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/profile"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.JSONResponse(201, UploadReplyJSON())).WithBodyValidation(rules)
	return foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ProfileInput]) (UploadReply, error) {
		return service.Save(ctx, input.Body)
	}))
}

// ReadContents tests the native streaming boundary. A storage adapter can use
// the same reader; this fixture consumes bytes without claiming persistence.
func ReadContents(ctx context.Context, file foundryhttp.UploadedFile) (int64, error) {
	reader, err := file.Open(ctx)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	return io.Copy(io.Discard, reader)
}
