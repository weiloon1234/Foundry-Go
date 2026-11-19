package teamworkflow

import "github.com/weiloon1234/Foundry-Go/validation"

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/http"
)

//foundry:form
type FormInput struct {
	Name string `form:"name"`
}

//foundry:multipart http=body
type MultipartInput struct {
	Name string `form:"name"`
}

//foundry:dto http=response
type RuleReply struct {
	Name string `json:"name"`
}

// These explicitly public, side-effect-free validation probes share the same
// domain value rule. Their source decoders preserve distinct wire semantics.
func RuleRoutes() []http.RouteRegistration {
	route := func(id http.RouteID, path string, method http.Method) http.Route[http.NoPath] {
		return http.DefineRoute(http.RouteSpec{ID: id, Method: method, Access: http.Public}, http.StaticPath(path))
	}

	jsonRoute := route("workflow.rules.json", "/rules/json", http.POST)
	json := http.DefineEndpoint(jsonRoute, http.EmptyQuery(), SubmissionInputBody(), RuleReplyResponse(200))

	json = http.ValidateBody(json, validation.Rules(SubmissionInputValidationFields().Name, NameRules))

	formRoute := route("workflow.rules.form", "/rules/form", http.POST)
	form := http.DefineEndpoint(formRoute, http.EmptyQuery(), http.FormBody(FormInputDescriptor()), RuleReplyResponse(200))

	form = http.ValidateBody(form, validation.Rules(FormInputValidationFields().Name, NameRules))

	queryRoute := route("workflow.rules.query", "/rules/query", http.GET)
	query := http.DefineEndpoint(queryRoute, FormInputDescriptor(), http.EmptyBody(), RuleReplyResponse(200))

	query = http.ValidateQuery(query, validation.Rules(FormInputValidationFields().Name, NameRules))

	multipartRoute := route("workflow.rules.multipart", "/rules/multipart", http.POST)
	multipart := http.DefineEndpoint(multipartRoute, http.EmptyQuery(), MultipartInputBody(""), RuleReplyResponse(200))

	multipart = http.ValidateBody(multipart, validation.Rules(MultipartInputValidationFields().Name, NameRules))

	return []http.RouteRegistration{
		json.Handle(func(_ context.Context, in http.Input[http.NoPath, http.NoQuery, SubmissionInput]) (RuleReply, error) {
			return RuleReply{Name: in.Body.Name}, nil
		}),
		form.Handle(func(_ context.Context, in http.Input[http.NoPath, http.NoQuery, FormInput]) (RuleReply, error) {
			return RuleReply{Name: in.Body.Name}, nil
		}),
		query.Handle(func(_ context.Context, in http.Input[http.NoPath, FormInput, http.NoBody]) (RuleReply, error) {
			return RuleReply{Name: in.Query.Name}, nil
		}),
		multipart.Handle(func(_ context.Context, in http.Input[http.NoPath, http.NoQuery, MultipartInput]) (RuleReply, error) {
			return RuleReply{Name: in.Body.Name}, nil
		}),
	}
}
