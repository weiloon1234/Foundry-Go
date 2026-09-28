package inspection_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/inspection"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Work struct {
	Number int `json:"number"`
}
type settings struct {
	Password secret.String
	Name     string
}

func TestInspectionReusesDeclarationsWithoutConstructingOrExecuting(t *testing.T) {
	provider := foundation.Module{Name: "pure", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, foundation.NewKey[string]("unused.service"), func(foundation.Resolver) (string, error) { t.Error("inspection constructed a service"); return "", nil })
	}, OnBoot: func(context.Context, *foundation.Runtime) error { t.Error("inspection booted services"); return nil }}
	builder := foundry.New().Register(provider)
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "status", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/status")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Error("inspection executed an endpoint")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Define[Work]("work", 1, jobs.DefaultPolicy("default"))
	declaration, err := job.Declare(func(context.Context, Work) error { t.Error("inspection executed a job"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	jobRegistry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	scheduled, err := schedule.Every("hourly", time.Hour, func(context.Context, schedule.Invocation) error {
		t.Error("inspection executed a schedule")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	options := scheduled.Options()
	options.Environments = []string{"testing"}
	scheduled, err = scheduled.With(options)
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := schedule.NewRegistry(scheduled)
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	password := config.Secret("db.password", func(s *settings) *secret.String { return &s.Password })
	name := config.String("app.name", func(s *settings) *string { return &s.Name })
	schema, err := config.New(password, name)
	if err != nil {
		t.Fatal(err)
	}
	_, provenance, err := schema.Load(settings{Password: secret.New("private-password"), Name: "private-name"}, config.Inputs[settings]{})
	if err != nil {
		t.Fatal(err)
	}
	sources := inspection.Sources{Builder: builder, HTTP: router, Jobs: jobRegistry, Schedules: schedules, Contracts: contracts, Configuration: []config.Report{provenance}}
	report, err := inspection.Collect(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Routes) != 1 || len(report.Jobs) != 1 || len(report.Schedules) != 1 || len(report.Assembly.Providers) != 1 || len(report.Contracts.HTTP) != 1 || len(report.Configuration) != 2 {
		t.Fatal("inspection omitted declarations")
	}
	report.Jobs[0].Policy.Backoff[0] = 0
	report.Schedules[0].Environments[0] = "changed"
	report.Routes[0].ID = "changed"
	again, err := inspection.Collect(t.Context(), sources)
	if err != nil || again.Jobs[0].Policy.Backoff[0] == 0 || again.Schedules[0].Environments[0] != "testing" || again.Routes[0].ID != "status" {
		t.Fatal("inspection exposed registry-owned state", err)
	}
	var output strings.Builder
	if err := inspection.Write(t.Context(), &output, again, inspection.Arguments{Section: inspection.All, Format: inspection.JSON}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "private-password") || strings.Contains(output.String(), "private-name") || !strings.Contains(output.String(), "db.password") {
		t.Fatal("configuration provenance disclosed setting values or omitted names")
	}
	var commands *cli.Registry
	inspect, err := inspection.Command("inspect", func(ctx context.Context) (inspection.Report, error) {
		sources.Commands = commands
		return inspection.Collect(ctx, sources)
	})
	if err != nil {
		t.Fatal(err)
	}
	commands, err = cli.New(inspect)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := commands.Parse([]string{"inspect", "--section", "commands", "--format", "json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &output, Err: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name":"inspect"`) {
		t.Fatal("inspection duplicated or lost its final command registry")
	}
}
