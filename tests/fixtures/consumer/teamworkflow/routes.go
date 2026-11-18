package teamworkflow

import (
	"context"
	"strings"

	"foundry.test/consumer/genericdto"
	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var Rejected = http.DefineError("workflow_rejected", 422, "The submission was rejected.")
var NameRules = validation.All(validation.NonBlank[string](), validation.MinLength[string](2), validation.MaxLength[string](40))

func projectResolver[P any](db database.Executor, team func(P) int64, slug func(P) Slug) modelbinding.Resolver[P, Resources] {
	parents := modelbinding.ByKey(db, orm.Keyed(Teams.Query(), Teams.Key()), team)
	return modelbinding.ThroughRelation(db, parents, TeamProjects, orm.TextExpr(Projects.Slug), slug)
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

	workflowPatchEndpoint := http.DefineEndpoint(route, http.EmptyQuery(), PatchBody(), http.JSONResponse(200, genericdto.EnvelopeJSON(ActionJSON())))
	workflowPatchEndpoint = http.ValidateBody(workflowPatchEndpoint,
		validation.Rules(fields.Title, validation.Optional(validation.Nullable(validation.MaxLength[string](64)))), validation.Rules(fields.Budget, validation.Optional(validation.Min(int64(0)))))

	return http.PrepareEndpoint(workflowPatchEndpoint, func(_ context.Context, in http.Input[ProjectPath, http.NoQuery, Patch]) (http.NoQuery, Patch, error) {
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

	workflowSubmitEndpoint := http.DefineEndpoint(route, http.EmptyQuery(), SubmissionInputBody(), http.JSONResponse(202, genericdto.EnvelopeJSON(ActionJSON())))
	workflowSubmitEndpoint = http.ConfigureEndpoint(workflowSubmitEndpoint, http.Errors(Rejected))
	workflowSubmitEndpoint = http.ValidateBody(workflowSubmitEndpoint, validation.Rules(SubmissionInputValidationFields().Name, NameRules))

	return http.PrepareEndpoint(workflowSubmitEndpoint, func(_ context.Context, in http.Input[SubmissionPath, http.NoQuery, SubmissionInput]) (http.NoQuery, SubmissionInput, error) {
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

	guardWorkflowPatch := http.RequireAuthentication(PatchEndpoint(), transport, guard)
	workflowPatchBinding := http.ForAuthenticatedBinding(guardWorkflowPatch)
	patch := modelbinding.BindAuthenticated(workflowPatchBinding, resolver)
	patch = modelbinding.AuthorizeAuthenticatedResource(patch,
		func(_ context.Context, actor Actor, in PatchRequest) error { return authorized(actor, in.Model) })

	workflowShowRoute := http.DefineRoute(http.RouteSpec{ID: "workflow.show", Method: http.GET, Access: http.Guarded}, ProjectPathDescriptor())
	show := http.DefineEndpoint(workflowShowRoute, http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, genericdto.EnvelopeJSON(ProjectViewJSON())))

	guardWorkflowShow := http.RequireAuthentication(show, transport, guard)
	workflowShowBinding := http.ForAuthenticatedBinding(guardWorkflowShow)
	boundShow := modelbinding.BindAuthenticated(workflowShowBinding, resolver)
	boundShow = modelbinding.AuthorizeAuthenticatedResource(boundShow,
		func(_ context.Context, actor Actor, in modelbinding.Input[ProjectPath, http.NoQuery, http.NoBody, Resources]) error {
			return authorized(actor, in.Model)
		})

	submissions := projectResolver(s.db, func(p SubmissionPath) int64 { return p.Team }, func(p SubmissionPath) Slug { return p.Project })

	required := http.RequireAuthentication(SubmitEndpoint(), transport, guard)
	required = http.AuthorizeActor(required, func(_ context.Context, _ Actor, in http.Input[SubmissionPath, http.NoQuery, SubmissionInput]) error {
		if in.Body.Name == "request-denied" {
			return http.Forbidden
		}
		return nil
	})

	workflowSubmitReplay := http.IdempotentAuthenticated(required, s.store, idempotency.Definition{ID: "workflow.submit", Version: 1})
	submit := modelbinding.BindAuthenticatedIdempotent(workflowSubmitReplay, submissions)
	submit = modelbinding.AuthorizeAuthenticatedIdempotentResource(submit, func(_ context.Context, actor Actor, in SubmissionRequest) error { return authorized(actor, in.Model) })

	workflowCatalogueRoute := http.DefineRoute(http.RouteSpec{ID: "workflow.catalogue", Method: http.GET, Access: http.Public}, http.StaticPath("/catalogue"))
	catalogue := pagination.DefineNumbered(workflowCatalogueRoute, CatalogueFiltersDescriptor(), CatalogueItemJSON(), pagination.DefaultConfig())

	routes := []http.RouteRegistration{
		patch.Handle(s.Patch), submit.Handle(submissionScope, s.Submit),
		boundShow.Handle(func(_ context.Context, _ Actor, in modelbinding.Input[ProjectPath, http.NoQuery, http.NoBody, Resources]) (ProjectEnvelope, error) {
			return ProjectEnvelope{Data: present(in.Model.Child), Trace: "workflow"}, nil
		}),
		pagination.Handle(catalogue, func(ctx context.Context, in pagination.Request[http.NoPath, CatalogueFilters]) (orm.PageResult[CatalogueItem], error) {
			rows := Projects.Query().Where(orm.ColumnEq(Projects.Enabled, true)).OrderBy(orm.KeyAsc(Projects.Key()))
			if team, set := in.Filters.Team.Get(); set {
				rows = rows.Where(orm.NumberEq(Projects.TeamID, team))
			}
			page, err := orm.Paginate(ctx, s.db, rows, in.Page)
			if err != nil {
				return orm.PageResult[CatalogueItem]{}, err
			}
			return pagination.MapPage(ctx, page, func(row Project) (CatalogueItem, error) { return CatalogueItem{Slug: row.Slug}, nil })
		}),
	}
	return append(routes, RuleRoutes()...), nil
}
