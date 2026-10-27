package application_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestAuditUsageDiagnosticSurvivesCLIKernel(t *testing.T) {
	declaration, err := application.AuditCommand()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	invocation, err := registry.Parse([]string{"audit", "prune", "--before", time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)}, &output)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	// The future cutoff must fail before touching this scope's database.
	resource := foundation.Module{Name: "audit.fixture", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, application.AuditScopeKey, &audit.Scope{})
	}, OnBoot: func(_ context.Context, runtime *foundation.Runtime) error {
		return runtime.OnShutdown("audit.fixture", func(context.Context) error { closed = true; return nil })
	}}
	streams := cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}
	module := cli.Module("commands", foundation.NewKey[*cli.Registry]("audit.fixture.commands"), registry, invocation, streams, resource.Name)
	app, err := foundation.NewBuilder(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(resource, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = app.Run(ctx, foundation.CLI)
	want := "run kernel cli: --before must not be later than the current time"
	var usage *cli.UsageError
	if err == nil || err.Error() != want || !errors.As(err, &usage) || !errors.Is(err, usage) || cli.Status(err) != cli.InvalidUsage || !closed || app.State() != foundation.Stopped || output.Len() != 0 {
		t.Fatal("audit command lost usage diagnostic, classification or lifecycle cleanup")
	}
	*usage = *cli.Usage("private replacement").(*cli.UsageError)
	if cli.Report(&output, err) != cli.InvalidUsage || output.String() != want+"\n" {
		t.Fatal("kernel diagnostic followed a mutable public usage error")
	}
}
