package httpuploads

import (
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func uploadEndpoint(route foundryhttp.Route[foundryhttp.NoPath], body foundryhttp.Body[ProfileInput]) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, ProfileInput, foundryhttp.NoContent] {
	// doc-example:begin http-endpoints-6
	limits := foundryhttp.DefaultEndpointLimits()
	limits.Multipart.Bytes, limits.Multipart.FileBytes = 520<<20, 512<<20

	upload := foundryhttp.DefineEndpoint(
		route, foundryhttp.EmptyQuery(), body, foundryhttp.EmptyResponse(204),
		foundryhttp.Limits(limits), // Allow multipart overhead beyond the 512 MiB file.
		foundryhttp.BodyLimit(520<<20),
		foundryhttp.Timeout(10*time.Minute),
	)
	// doc-example:end http-endpoints-6
	return upload
}
