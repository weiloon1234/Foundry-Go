package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func New(dependencies datatable.Dependencies, config datatable.Config) (*datatable.Manager, error) {
	return datatable.New(dependencies, config, Members.Registration(), Orders.Registration(), Totals.Registration())
}

func ListMembers(ctx context.Context, manager *datatable.Manager, guard auth.Guard[Operator], request datatable.Request) (query.Page[MemberRow], error) {
	return Members.Query(ctx, manager, FromGuard(guard), request)
}

// DownloadRoute composes the existing authenticated endpoint and seekable file
// response. The framework owns response ranges and artifact cleanup. The route
// declares the manager's download deadline, which covers generation and transfer.
func DownloadRoute(manager *datatable.Manager, transport *foundryhttp.Authentication, guard auth.Guard[Operator], presentation datatable.Presentation) foundryhttp.RouteRegistration {
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.members.csv", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/reports/members.csv")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/csv; charset=utf-8"),
	).WithTimeout(manager.DownloadTimeout())
	return foundryhttp.RequireAuthentication(endpoint, transport, guard).Handle(func(ctx context.Context, _ Operator, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
		return Members.Download(ctx, manager, FromGuard(guard), datatable.Request{}, datatable.ExportOptions{Format: datatable.CSV, Presentation: presentation})
	})
}
