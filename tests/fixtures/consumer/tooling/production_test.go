package tooling_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"foundry.test/consumer/models"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestProductionCompositionUsesConcreteOperatorAndSharedRuntime(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	app, err := foundation.NewBuilder(foundation.WithObservability(recorder)).Register(
		tooling.ReadinessModule(nil, func(foundation.Resolver) ([]health.Probe, error) {
			return []health.Probe{{ID: "test.dependency", Check: func(context.Context) error { return nil }}}, nil
		}),
		tooling.DiagnosticsModule(),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	status, err := foundation.Resolve(app.Services(), tooling.DiagnosticsKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ready, err := status.Readiness(t.Context()); err != nil || !ready.Ready || !status.Liveness().Live {
		t.Fatal("consumer lifecycle was not ready", err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	provider := auth.DefineProvider("operators", user.FoundryReference(), func(context.Context, model.ID[models.User]) (value.Optional[models.User], error) {
		return value.Set(user), nil
	}, func(context.Context, models.User) (bool, error) { return true, nil })
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[models.User, model.ID[models.User]]], error) {
		if token.Reveal() == "fixture-operator" {
			return value.Set(proof), nil
		}
		return value.Optional[auth.Proof[models.User, model.ID[models.User]]]{}, nil
	})
	guard := auth.DefineGuard("operators", provider, strategy)
	permission := auth.DefinePermission("operations.read", func(_ context.Context, user models.User) (bool, error) {
		return user.Status == models.StatusActive, nil
	})
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	routes, paths, err := tooling.OperationalRoutes(status, authentication, guard, permission)
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(routes...)
	if err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultServerConfig()
	config.MaintenanceReadPaths = paths
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		for _, authenticated := range []bool{false, true} {
			request := httptest.NewRequest("GET", path, nil)
			if authenticated {
				request.Header.Set("Authorization", "Bearer fixture-operator")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := 401
			if authenticated {
				want = 200
			}
			if response.Code != want {
				t.Fatal("consumer diagnostics authentication/status", path, response.Code)
			}
		}
	}
	if err := tooling.ObservedTask(t.Context(), recorder, "consumer.work", func(ctx context.Context) error {
		if tracing.FromContext(ctx).IsZero() {
			t.Error("consumer lost operation trace")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	if ready, err := status.Readiness(t.Context()); err != nil || ready.Ready || !status.Liveness().Live {
		t.Fatal("consumer maintenance conflated readiness and liveness", err)
	}
	if snapshot := recorder.Snapshot(); snapshot.Active != 0 || snapshot.Completed == 0 {
		t.Fatal("consumer observations missing")
	}
}
