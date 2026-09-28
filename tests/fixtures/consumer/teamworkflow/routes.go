package teamworkflow

import (
	"context"
	"foundry.test/consumer/genericdto"
	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
)

var Rejected = http.DefineError("workflow_rejected", 422, "The submission was rejected.")
var NameRules = validation.All(validation.NonBlank[string](), validation.MinLength[string](2), validation.MaxLength[string](40))

func projectResolver[P any](db database.Executor, team func(P) int64, slug func(P) Slug) modelbinding.Resolver[P, Resources] {
	parents := modelbinding.ByKey(db, QueryWorkflowTeams(), team)
	return modelbinding.Through(db, parents, TeamRelations().Projects, ProjectFields().Slug, slug)
}
func authorized(actor Actor, resources Resources) error {
	if actor.Tenant != resources.Parent.Tenant || !resources.Child.Enabled {
		return http.Forbidden
	}
	return nil
}
func PatchEndpoint() http.Endpoint[ProjectPath, http.NoQuery, Patch, ActionEnvelope] {
	route := http.DefineRoute(http.RouteSpec{ID: "workflow.patch", Method: http.PATCH, Access: http.Guarded}, ProjectPathDescriptor())
	fields := PatchValidationFields()
	return http.DefineEndpoint(route, http.EmptyQuery(), http.JSONBody(PatchJSON()), http.JSONResponse(200, genericdto.EnvelopeJSON(ActionJSON()))).
		WithBodyValidation(fields.Title.Rules(validation.Optional(validation.Nullable(validation.MaxLength[string](64)))), fields.Budget.Rules(validation.Optional(validation.Min(int64(0))))).
		WithPreparation(func(_ context.Context, in http.Input[ProjectPath, http.NoQuery, Patch]) (http.NoQuery, Patch, error) {
			if title, set := in.Body.Title.Get(); set {
				if text, present := title.Get(); present {
					in.Body.Title = value.Set(value.Of(strings.TrimSpace(text)))
				}
			}
			return in.Query, in.Body, nil
		})
}
func SubmitEndpoint() http.Endpoint[SubmissionPath, http.NoQuery, SubmissionInput, ActionEnvelope] {
	route := http.DefineRoute(http.RouteSpec{ID: "workflow.submit", Method: http.POST, Access: http.Guarded}, SubmissionPathDescriptor())
	return http.DefineEndpoint(route, http.EmptyQuery(), http.JSONBody(SubmissionInputJSON()), http.JSONResponse(202, genericdto.EnvelopeJSON(ActionJSON()))).
		WithErrors(Rejected).WithBodyValidation(SubmissionInputValidationFields().Name.Rules(NameRules)).
		WithPreparation(func(_ context.Context, in http.Input[SubmissionPath, http.NoQuery, SubmissionInput]) (http.NoQuery, SubmissionInput, error) {
			in.Body.Name = strings.TrimSpace(in.Body.Name)
			return in.Query, in.Body, nil
		})
}
func (s *Service) Routes() ([]http.RouteRegistration, error) {
	transport, guard, err := authfixture.Authentication()
	if err != nil {
		return nil, err
	}
	resolver := projectResolver(s.db, func(p ProjectPath) int64 { return p.Team }, func(p ProjectPath) Slug { return p.Project })
	patch := modelbinding.BindAuthenticated(http.RequireAuthentication(PatchEndpoint(), transport, guard), resolver).
		WithAuthorization(func(_ context.Context, actor Actor, in PatchRequest) error { return authorized(actor, in.Model) })
	show := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "workflow.show", Method: http.GET, Access: http.Guarded}, ProjectPathDescriptor()), http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, genericdto.EnvelopeJSON(ProjectViewJSON())))
	boundShow := modelbinding.BindAuthenticated(http.RequireAuthentication(show, transport, guard), resolver).
		WithAuthorization(func(_ context.Context, actor Actor, in modelbinding.Input[ProjectPath, http.NoQuery, http.NoBody, Resources]) error {
			return authorized(actor, in.Model)
		})
	submissions := projectResolver(s.db, func(p SubmissionPath) int64 { return p.Team }, func(p SubmissionPath) Slug { return p.Project })
	required := http.RequireAuthentication(SubmitEndpoint(), transport, guard).WithAuthorization(func(_ context.Context, _ Actor, in http.Input[SubmissionPath, http.NoQuery, SubmissionInput]) error {
		if in.Body.Name == "request-denied" {
			return http.Forbidden
		}
		return nil
	})
	submit := modelbinding.BindAuthenticated(required, submissions).
		WithAuthorization(func(_ context.Context, actor Actor, in SubmissionRequest) error { return authorized(actor, in.Model) }).
		Idempotent(s.store, idempotency.Definition{ID: "workflow.submit", Version: 1})
	catalogue := pagination.DefineNumbered(http.DefineRoute(http.RouteSpec{ID: "workflow.catalogue", Method: http.GET, Access: http.Public}, http.StaticPath("/catalogue")), CatalogueFiltersDescriptor(), CatalogueItemJSON(), pagination.DefaultConfig())
	routes := []http.RouteRegistration{
		patch.Handle(s.Patch), submit.Handle(submissionScope, s.Submit),
		boundShow.Handle(func(_ context.Context, _ Actor, in modelbinding.Input[ProjectPath, http.NoQuery, http.NoBody, Resources]) (ProjectEnvelope, error) {
			return ProjectEnvelope{Data: present(in.Model.Child), Trace: "workflow"}, nil
		}),
		catalogue.Handle(func(ctx context.Context, in pagination.Request[http.NoPath, CatalogueFilters]) (query.Page[CatalogueItem], error) {
			rows := QueryWorkflowProjects().Where(ProjectFields().Enabled.Eq(true)).OrderBy(ProjectFields().ID.Asc())
			if team, set := in.Filters.Team.Get(); set {
				rows = rows.Where(ProjectFields().TeamID.Eq(team))
			}
			page, err := rows.Paginate(ctx, s.db, in.Page)
			if err != nil {
				return query.Page[CatalogueItem]{}, err
			}
			return pagination.MapPage(ctx, page, func(row Project) (CatalogueItem, error) { return CatalogueItem{Slug: row.Slug}, nil })
		}),
	}
	return append(routes, RuleRoutes()...), nil
}
