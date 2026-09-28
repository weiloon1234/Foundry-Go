package httpdownloads

import (
	"context"
	"os"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// ReportRoute is the complete minimal example used by the download guide. The
// application owns root; the framework opens and closes each response file.
func ReportRoute(root *os.Root) (*foundryhttp.Router, error) {
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.download", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/report")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("application/pdf"),
	)
	return foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return foundryhttp.LocalDownload(root, "reports/monthly.pdf").WithName("report.pdf").WithMediaType("application/pdf"), nil
	}))
}
