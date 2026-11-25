// Package requestflow demonstrates typed forms and request preparation using
// the same value rules as JSON, query and multipart input.
package requestflow

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin submission-form

//foundry:form
type Submission struct {
	Name    string                 `form:"name"`
	Title   value.Optional[string] `form:"title"`
	Blocked value.Optional[string] `form:"blocked"`
	Tags    []string               `form:"tags[]"`
}

// doc-example:end submission-form

//foundry:dto http=body
type JSONSubmission struct {
	Name string `json:"name"`
}

//foundry:multipart http=body
type MultipartSubmission struct {
	Name string `form:"name"`
}

// doc-example:begin submission-contracts

//foundry:dto http=response
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

// doc-example:end submission-contracts

func submissionEndpoint(access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, Search, Submission, Reply] {
	form := formEndpoint(access)
	limits := foundryhttp.DefaultEndpointLimits()
	// Deliberately tiny unused JSON-body allowance proves form budget independence.
	limits.Body.Bytes = 1
	limits.Form = foundryhttp.QueryLimits{Bytes: 512, Pairs: 16, Issues: 4}
	return foundryhttp.ConfigureEndpoint(form, foundryhttp.Limits(limits))
}

// doc-example:begin prepare-submission
func Prepare(_ context.Context, input Request) (Search, Submission, error) {
	body := input.Body
	body.Name = strings.TrimSpace(body.Name)
	if !body.Title.IsSet() {
		body.Title = value.Set("Member")
	}
	return input.Query, body, nil
}

// doc-example:end prepare-submission

var Submit = func() foundryhttp.Endpoint[foundryhttp.NoPath, Search, Submission, Reply] {
	formsSubmitEndpoint := submissionEndpoint(foundryhttp.Public)
	formsSubmitEndpoint = foundryhttp.PrepareEndpoint(formsSubmitEndpoint, Prepare)

	return foundryhttp.AuthorizeEndpoint(formsSubmitEndpoint, func(_ context.Context, input Request) error {
		if input.Body.Name == "denied" {
			return foundryhttp.Forbidden
		}
		return nil
	})
}()

func Handle(_ context.Context, input Request) (Reply, error) {
	title, _ := input.Body.Title.Get()
	query, _ := input.Query.Name.Get()
	return Reply{Name: input.Body.Name, Title: title, Query: query, Tags: input.Body.Tags}, nil
}

// Named guards select concrete models at assembly. Different actors retain
// different callback signatures; no actor is obtained through context casts.
// doc-example:begin submission-actor
type Account struct {
	ID      int64
	Enabled bool
}

// doc-example:end submission-actor

func AccountEndpoint(transport *foundryhttp.Authentication, guard auth.Guard[Account]) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, Search, Submission, Account, Reply] {
	formsSubmitEndpoint := submissionEndpoint(foundryhttp.Guarded)
	guardFormsSubmit := foundryhttp.RequireAuthentication(formsSubmitEndpoint, transport, guard)

	guardFormsSubmit = foundryhttp.PrepareAuthenticated(guardFormsSubmit, Prepare)

	return foundryhttp.AuthorizeActor(guardFormsSubmit, func(_ context.Context, actor Account, input Request) error {
		if !actor.Enabled || input.Body.Name == "denied" {
			return foundryhttp.Forbidden
		}
		return nil
	})
}
func GuestEndpoint(transport *foundryhttp.Authentication, guard auth.Guard[Account]) foundryhttp.OptionalAuthenticationEndpoint[foundryhttp.NoPath, Search, Submission, Account, Reply] {
	formsSubmitEndpoint := submissionEndpoint(foundryhttp.Public)
	guardFormsSubmit := foundryhttp.OptionalAuthentication(formsSubmitEndpoint, transport, guard)

	guardFormsSubmit = foundryhttp.PrepareOptional(guardFormsSubmit, Prepare)

	return foundryhttp.AuthorizeOptionalActor(guardFormsSubmit, func(_ context.Context, actor value.Optional[Account], input Request) error {
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

	jsonRoute := route("rules.json", "/json", foundryhttp.POST)
	json := foundryhttp.DefineEndpoint(jsonRoute, foundryhttp.EmptyQuery(), JSONSubmissionBody(), foundryhttp.EmptyResponse(204))

	json = foundryhttp.ValidateBody(json, validation.Rules(JSONSubmissionValidationFields().Name, NameRules))

	multiRoute := route("rules.multipart", "/multipart", foundryhttp.POST)
	multi := foundryhttp.DefineEndpoint(multiRoute, foundryhttp.EmptyQuery(), MultipartSubmissionBody(""), foundryhttp.EmptyResponse(204))

	multi = foundryhttp.ValidateBody(multi, validation.Rules(MultipartSubmissionValidationFields().Name, NameRules))

	queryRoute := route("rules.query", "/query", foundryhttp.GET)
	query := foundryhttp.DefineEndpoint(queryRoute, SubmissionDescriptor(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	query = foundryhttp.ValidateQuery(query, validation.Rules(SubmissionValidationFields().Name, NameRules))

	formRoute := route("rules.form", "/form", foundryhttp.POST)
	form := foundryhttp.DefineEndpoint(formRoute, foundryhttp.EmptyQuery(), foundryhttp.FormBody(SubmissionDescriptor()), foundryhttp.EmptyResponse(204))

	form = foundryhttp.ValidateBody(form, validation.Rules(SubmissionValidationFields().Name, NameRules))

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
