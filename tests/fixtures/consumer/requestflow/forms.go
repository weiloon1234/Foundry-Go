// Package requestflow demonstrates typed forms and request preparation using
// the same value rules as JSON, query and multipart input.
package requestflow

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
)

//foundry:form
type Submission struct {
	Name    string                 `form:"name"`
	Title   value.Optional[string] `form:"title"`
	Blocked value.Optional[string] `form:"blocked"`
	Tags    []string               `form:"tags[]"`
}

//foundry:dto
type JSONSubmission struct {
	Name string `json:"name"`
}

//foundry:multipart
type MultipartSubmission struct {
	Name string `form:"name"`
}

//foundry:dto
type Reply struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Query string   `json:"query"`
	Tags  []string `json:"tags,omitzero"`
}

// The URL query uses its own typed value, even where a wire name matches a body field.
type Search struct{ Name value.Optional[string] }
type Request = foundryhttp.Input[foundryhttp.NoPath, Search, Submission]

var NameRules = validation.All(validation.NonBlank[string](), validation.MinLength[string](2), validation.MaxLength[string](40))

func endpoint(access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, Search, Submission, Reply] {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "forms.submit", Method: foundryhttp.POST, Access: access}, foundryhttp.StaticPath("/form"))
	search := foundryhttp.DefineQuery(foundryhttp.OptionalQueryParam("name", foundryhttp.StringQuery[string](), func(q *Search) *value.Optional[string] { return &q.Name }))
	fields := SubmissionValidationFields()
	limits := foundryhttp.DefaultEndpointLimits()
	// Deliberately tiny unused JSON-body allowance proves form budget independence.
	limits.Body.Bytes = 1
	limits.Form = foundryhttp.QueryLimits{Bytes: 512, Pairs: 16, Issues: 4}
	return foundryhttp.DefineEndpoint(route, search, foundryhttp.FormBody(SubmissionDescriptor()), foundryhttp.JSONResponse(201, ReplyJSON())).WithLimits(limits).WithBodyValidation(
		fields.Name.Rules(NameRules), fields.Title.Rules(validation.Required(validation.MinLength[string](2))), fields.Blocked.Rules(validation.Absent[string]()),
	)
}

func Prepare(_ context.Context, input Request) (Search, Submission, error) {
	body := input.Body
	body.Name = strings.TrimSpace(body.Name)
	if !body.Title.IsSet() {
		body.Title = value.Set("Member")
	}
	return input.Query, body, nil
}

var Submit = endpoint(foundryhttp.Public).WithPreparation(Prepare).WithAuthorization(func(_ context.Context, input Request) error {
	if input.Body.Name == "denied" {
		return foundryhttp.Forbidden
	}
	return nil
})

func Handle(_ context.Context, input Request) (Reply, error) {
	title, _ := input.Body.Title.Get()
	query, _ := input.Query.Name.Get()
	return Reply{Name: input.Body.Name, Title: title, Query: query, Tags: input.Body.Tags}, nil
}

// Named guards select concrete models at assembly. Different actors retain
// different callback signatures; no actor is obtained through context casts.
type Account struct {
	ID      int64
	Enabled bool
}

func AccountEndpoint(transport *foundryhttp.Authentication, guard auth.Guard[Account]) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, Search, Submission, Account, Reply] {
	return foundryhttp.RequireAuthentication(endpoint(foundryhttp.Guarded), transport, guard).WithPreparation(Prepare).WithAuthorization(func(_ context.Context, actor Account, input Request) error {
		if !actor.Enabled || input.Body.Name == "denied" {
			return foundryhttp.Forbidden
		}
		return nil
	})
}
func GuestEndpoint(transport *foundryhttp.Authentication, guard auth.Guard[Account]) foundryhttp.OptionalAuthenticationEndpoint[foundryhttp.NoPath, Search, Submission, Account, Reply] {
	return foundryhttp.OptionalAuthentication(endpoint(foundryhttp.Public), transport, guard).WithPreparation(Prepare).WithAuthorization(func(_ context.Context, actor value.Optional[Account], input Request) error {
		if selected, ok := actor.Get(); ok && !selected.Enabled {
			return foundryhttp.Forbidden
		}
		return nil
	})
}

func RuleSourceRouter() (*foundryhttp.Router, error) {
	route := func(id foundryhttp.RouteID, path string, method foundryhttp.Method) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	json := foundryhttp.DefineEndpoint(route("rules.json", "/json", foundryhttp.POST), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(JSONSubmissionJSON()), foundryhttp.EmptyResponse(204)).WithBodyValidation(JSONSubmissionValidationFields().Name.Rules(NameRules))
	multi := foundryhttp.DefineEndpoint(route("rules.multipart", "/multipart", foundryhttp.POST), foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(MultipartSubmissionDescriptor()), foundryhttp.EmptyResponse(204)).WithBodyValidation(MultipartSubmissionValidationFields().Name.Rules(NameRules))
	query := foundryhttp.DefineEndpoint(route("rules.query", "/query", foundryhttp.GET), SubmissionDescriptor(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204)).WithQueryValidation(SubmissionValidationFields().Name.Rules(NameRules))
	form := foundryhttp.DefineEndpoint(route("rules.form", "/form", foundryhttp.POST), foundryhttp.EmptyQuery(), foundryhttp.FormBody(SubmissionDescriptor()), foundryhttp.EmptyResponse(204)).WithBodyValidation(SubmissionValidationFields().Name.Rules(NameRules))
	return foundryhttp.NewRouter(
		json.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, JSONSubmission]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		multi.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, MultipartSubmission]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		query.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, Submission, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		form.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Submission]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
}
