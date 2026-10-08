package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/value"
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

// QueryRoute returns table errors unchanged; the framework owns client rejection
// classification and current guard/policy checks.
func QueryRoute(manager *datatable.Manager, transport *foundryhttp.Authentication, guard auth.Guard[Operator]) foundryhttp.RouteRegistration {
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.members.query", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/reports/members/query")),
		foundryhttp.EmptyQuery(), foundryhttp.JSONBody(datatable.RequestJSON()), foundryhttp.JSONResponse(200, pagination.NumberedJSON(MemberRowJSON())),
	)
	return foundryhttp.RequireAuthentication(endpoint, transport, guard).Handle(func(ctx context.Context, _ Operator, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, datatable.Request]) (pagination.NumberedResponse[MemberRow], error) {
		page, err := ListMembers(ctx, manager, guard, in.Body)
		if err != nil {
			return pagination.NumberedResponse[MemberRow]{}, err
		}
		rows := page.Items
		if rows == nil {
			rows = []MemberRow{}
		}
		return pagination.NumberedResponse[MemberRow]{
			Data:  rows,
			Meta:  pagination.NumberedMeta{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages},
			Links: pagination.Links{Next: value.Null[string](), Previous: value.Null[string]()},
		}, nil
	})
}
