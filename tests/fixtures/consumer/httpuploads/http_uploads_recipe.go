package httpuploads

import (
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin http-uploads-3
func largeUploadEndpoint(route foundryhttp.Route[foundryhttp.NoPath], directory string) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, ProfileInput, foundryhttp.NoContent] {

	limits := foundryhttp.DefaultEndpointLimits()
	limits.Multipart.Bytes, limits.Multipart.FileBytes = 520<<20, 512<<20
	upload := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), ProfileInputBody(directory), foundryhttp.EmptyResponse(204))
	upload = foundryhttp.ConfigureEndpoint(upload, foundryhttp.Limits(limits),
		foundryhttp.BodyLimit(520<<20), foundryhttp.Timeout(10*time.Minute))
	return upload
}

// doc-example:end http-uploads-3
