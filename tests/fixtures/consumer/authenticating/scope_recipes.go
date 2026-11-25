package authenticating

import (
	"context"
	"sync/atomic"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func manualRequestScope(ctx context.Context, api, web auth.Guard[models.User], credentials auth.Credentials, readOrder auth.Policy[models.User, models.Order]) (models.User, error) {
	// doc-example:begin manual-request-scope
	registry, err := auth.NewRegistry(auth.DefaultConfig(), api.Registration(), web.Registration(), readOrder.Registration())
	if err != nil {
		return models.User{}, err
	}
	scope, err := registry.NewScope(ctx, credentials)
	if err != nil {
		return models.User{}, err
	}
	defer scope.Close()
	ctx = scope.Context()
	return api.Require(ctx)
	// doc-example:end manual-request-scope
}

func configuredAuthorization(ctx context.Context, settings application.Settings, readOrder auth.Policy[models.User, models.Order]) (*application.App, error) {
	// doc-example:begin configured-authorization
	return application.New(settings).Register(application.Authorization("app.authorization",
		application.Authorize(ViewAccount), application.Authorize(readOrder),
	)).Build(ctx)
	// doc-example:end configured-authorization
}

func policyHooks(isSuperAdmin func(context.Context, models.User) (bool, error), readOnlyMode *atomic.Bool) (auth.Before[models.User], auth.After[models.User]) {
	// doc-example:begin policy-hooks
	superAdmin := auth.DefineBefore("users.super_admin",
		func(ctx context.Context, user models.User, _ auth.PolicyName) (auth.Verdict, error) {
			allowed, err := isSuperAdmin(ctx, user)
			if err != nil {
				return auth.Abstain, err
			}
			if allowed {
				return auth.Allow, nil
			}
			return auth.Abstain, nil
		})
	readOnly := auth.DefineAfter("users.read_only",
		func(_ context.Context, _ models.User, policy auth.PolicyName) (auth.Verdict, error) {
			if readOnlyMode.Load() && policy != "orders.read" {
				return auth.Deny, nil
			}
			return auth.Abstain, nil
		})
	// doc-example:end policy-hooks
	return superAdmin, readOnly
}

func attributedOperation(ctx context.Context, api auth.Guard[models.User]) (context.Context, attribution.Origin, error) {
	// doc-example:begin attributed-operation
	ctx, err := api.WithAttribution(ctx) // CLI/worker scope already explicitly verified.
	if err != nil {
		return ctx, attribution.Origin{}, err
	}
	// Existing audit.RecordFor and Topic.Enqueue read this attribution.
	origin, err := api.Origin(ctx)
	if err != nil {
		return ctx, attribution.Origin{}, err
	}
	// doc-example:end attributed-operation
	return ctx, origin, nil
}
