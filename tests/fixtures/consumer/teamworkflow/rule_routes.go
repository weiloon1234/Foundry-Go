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

//foundry:multipart
type MultipartInput struct {
	Name string `form:"name"`
}

//foundry:dto
type RuleReply struct {
	Name string `json:"name"`
}

// These explicitly public, side-effect-free validation probes share the same
// domain value rule. Their source decoders preserve distinct wire semantics.
func RuleRoutes() []http.RouteRegistration {
	route := func(id http.RouteID, path string, method http.Method) http.Route[http.NoPath] {
		return http.DefineRoute(http.RouteSpec{ID: id, Method: method, Access: http.Public}, http.StaticPath(path))
	}
	json := http.DefineEndpoint(route("workflow.rules.json", "/rules/json", http.POST), http.EmptyQuery(), http.JSONBody(SubmissionInputJSON()), http.JSONResponse(200, RuleReplyJSON())).WithBodyValidation(validation.Rules(SubmissionInputValidationFields().Name, NameRules))
	form := http.DefineEndpoint(route("workflow.rules.form", "/rules/form", http.POST), http.EmptyQuery(), http.FormBody(FormInputDescriptor()), http.JSONResponse(200, RuleReplyJSON())).WithBodyValidation(validation.Rules(FormInputValidationFields().Name, NameRules))
	query := http.DefineEndpoint(route("workflow.rules.query", "/rules/query", http.GET), FormInputDescriptor(), http.EmptyBody(), http.JSONResponse(200, RuleReplyJSON())).WithQueryValidation(validation.Rules(FormInputValidationFields().Name, NameRules))
	multipart := http.DefineEndpoint(route("workflow.rules.multipart", "/rules/multipart", http.POST), http.EmptyQuery(), http.MultipartBody(MultipartInputDescriptor()), http.JSONResponse(200, RuleReplyJSON())).WithBodyValidation(validation.Rules(MultipartInputValidationFields().Name, NameRules))
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
