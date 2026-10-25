package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type diagnosticFailureSink struct{}

func (*diagnosticFailureSink) RecordFailure(context.Context, jobs.FailedJob) error { return nil }

type privateJobConstructorDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateJobConstructorDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private job constructor diagnostic")
}

func (e *privateJobConstructorDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateJobConstructorDiagnostic) Unwrap() error            { return fault.Invalid }

func TestFailureSinkConstructionRetainsFrameworkDiagnostics(t *testing.T) {
	for _, stage := range []string{"nil", "typed nil", "private panic", "private goexit"} {
		t.Run(stage, func(t *testing.T) {
			cause := &privateJobConstructorDiagnostic{exit: stage == "private goexit"}
			key := foundation.NewKey[*jobs.Dispatcher]("jobs")
			register := func(r *foundation.Registrar) error {
				return jobs.RegisterFailureSink(r, key, "archive", func(foundation.Resolver) (jobs.FailureSink, error) {
					switch stage {
					case "nil":
						return nil, nil
					case "typed nil":
						var sink *diagnosticFailureSink
						return sink, nil
					default:
						return nil, cause
					}
				})
			}
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "sinks", OnRegister: register}).Build(t.Context())
			want := `construct service foundry.contribution."jobs.failure-sinks.\"jobs\""."archive": invalid: job failure sink constructor returned nil`
			if err == nil || !errors.Is(err, fault.Invalid) {
				t.Fatal("failure sink construction lost its rejection classification")
			}
			if stage == "private panic" || stage == "private goexit" {
				want = `construct service foundry.contribution."jobs.failure-sinks.\"jobs\""."archive" failed`
				var retained *privateJobConstructorDiagnostic
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("failure sink construction lost the application cause")
				}
			} else {
				var retained *fault.Error
				if !errors.As(err, &retained) || !errors.Is(err, retained) || retained.Code() != fault.Invalid || retained.Message() != "job failure sink constructor returned nil" {
					t.Fatal("failure sink construction lost its source fault")
				}
				*retained = *fault.Wrap(fault.Invalid, "private replacement", cause)
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, err); got != want {
					t.Fatalf("failure sink construction diagnostic = %q, want %q", got, want)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("failure sink construction formatted an application cause")
			}
		})
	}
}

func TestJobsModuleNamespaceDiagnosticSurvivesRegistration(t *testing.T) {
	key := foundation.NewKey[*jobs.Dispatcher]("jobs")
	namespace := keyspace.Namespace{Application: "diagnostic", Environment: "test"}
	other := keyspace.Namespace{Application: "different", Environment: "test"}
	var constructions atomic.Int32
	module := jobs.Module("jobs", key, jobs.DefaultDispatchConfig(namespace), jobs.DefaultWorkerConfig(other, "default"), nil, func(foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		constructions.Add(1)
		return nil, nil, nil
	})
	_, err := foundation.NewBuilder().Register(module).Build(t.Context())
	var retained *fault.Error
	if err == nil || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || !errors.Is(err, retained) {
		t.Fatal("job module registration lost the namespace failure")
	}
	*retained = *fault.New(fault.Invalid, "private namespace replacement")
	want := "register provider jobs: invalid: job module dispatcher and worker must share a namespace"
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(format, err); got != want {
			t.Fatalf("job module registration diagnostic = %q, want %q", got, want)
		}
	}
	if constructions.Load() != 0 {
		t.Fatal("namespace rejection acquired a backend")
	}
}

func TestWorkerModuleNamespaceDiagnosticSurvivesConstruction(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	key := foundation.NewKey[*jobs.Dispatcher]("jobs")
	namespace := keyspace.Namespace{Application: "diagnostic", Environment: "test"}
	other := keyspace.Namespace{Application: "different", Environment: "test"}
	connection := jobs.ConnectionModule("connection", key, jobs.DefaultDispatchConfig(namespace), nil, func(foundation.Resolver) (jobs.Backend, []jobs.Declaration, error) {
		return backend, nil, nil
	})
	worker := jobs.WorkerModule("worker", key, jobs.DefaultWorkerConfig(other, "default"), []foundation.ProviderID{"connection"})
	_, err = foundation.NewBuilder().Register(connection, worker).Build(t.Context())
	var retained *fault.Error
	if err == nil || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained.Message() != "worker and dispatcher must share a namespace" {
		t.Fatal("worker module lost the namespace failure")
	}
	if !errors.Is(err, retained) {
		t.Fatal("worker module lost its source fault identity")
	}
	*retained = *fault.New(fault.Invalid, "private namespace replacement")
	want := "construct service foundry.jobs.worker.worker: invalid: worker and dispatcher must share a namespace"
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(format, err); got != want {
			t.Fatalf("worker module diagnostic = %q, want %q", got, want)
		}
	}
}
