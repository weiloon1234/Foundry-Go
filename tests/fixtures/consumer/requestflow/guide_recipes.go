package requestflow

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin form-endpoint
func formEndpoint(access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, Search, Submission, Reply] {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "forms.submit", Method: foundryhttp.POST, Access: access}, foundryhttp.StaticPath("/form"))
	search := foundryhttp.DefineQuery(foundryhttp.OptionalQueryParam("name", foundryhttp.StringQuery[string](), func(q *Search) *value.Optional[string] { return &q.Name }))
	form := foundryhttp.DefineEndpoint(route, search, foundryhttp.FormBody(SubmissionDescriptor()), ReplyResponse(201))
	fields := SubmissionValidationFields()
	return foundryhttp.ValidateBody(form,
		validation.Rules(fields.Name, NameRules),
		validation.Rules(fields.Title, validation.Required(validation.MinLength[string](2))),
		validation.Rules(fields.Blocked, validation.Absent[string]()),
	)
}

// doc-example:end form-endpoint

// doc-example:begin forms-request-lifecycle-2
func preparedSubmission(authorizeSubmission func(context.Context, Submission) error) foundryhttp.Endpoint[foundryhttp.NoPath, Search, Submission, Reply] {
	form := formEndpoint(foundryhttp.Public)
	form = foundryhttp.PrepareEndpoint(form, Prepare)
	form = foundryhttp.AuthorizeEndpoint(form, func(ctx context.Context, input Request) error {
		return authorizeSubmission(ctx, input.Body)
	})
	return form
}

// doc-example:end forms-request-lifecycle-2

func authorizedSubmission(transport *foundryhttp.Authentication, accountGuard auth.Guard[Account], accountPolicy func(context.Context, Account, Submission) error) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, Search, Submission, Account, Reply] {
	// doc-example:begin forms-request-lifecycle-3
	form := formEndpoint(foundryhttp.Guarded)
	prepared := foundryhttp.PrepareEndpoint(form, Prepare)
	secured := foundryhttp.RequireAuthentication(prepared, transport, accountGuard, foundryhttp.AnyActorOf(accountGuard))
	secured = foundryhttp.AuthorizeActor(secured, func(ctx context.Context, actor Account, input Request) error {
		return accountPolicy(ctx, actor, input.Body)
	})
	// doc-example:end forms-request-lifecycle-3
	return secured
}
