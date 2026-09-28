package datatable

import (
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestExportHandlerOwnsArtifactThroughDeliveryFailures(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	failure := errors.New("destination rejected report")
	for name, deliver := range map[string]func(context.Context, *Artifact) error{
		"success": func(_ context.Context, artifact *Artifact) error { _, err := io.Copy(io.Discard, artifact); return err },
		"error":   func(context.Context, *Artifact) error { return failure },
		"panic":   func(context.Context, *Artifact) error { panic("destination panic") },
		"Goexit":  func(context.Context, *Artifact) error { runtime.Goexit(); return nil },
	} {
		t.Run(name, func(t *testing.T) {
			var received *Artifact
			handler, err := ExportHandler(table, manager, func(context.Context, string) (reportActor, Request, ExportOptions, error) {
				return reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV}, nil
			}, func(ctx context.Context, payload string, artifact *Artifact) error {
				if payload != "actor reference" {
					return errors.New("job payload changed")
				}
				received = artifact
				return deliver(ctx, artifact)
			})
			if err != nil {
				t.Fatal(err)
			}
			err = handler(t.Context(), "actor reference")
			if (name == "success") != (err == nil) {
				t.Fatal("wrong delivery outcome", err)
			}
			if name == "error" && !errors.Is(err, failure) {
				t.Fatal("delivery error lost", err)
			}
			if received == nil {
				t.Fatal("complete artifact was not delivered")
			}
			if _, err := received.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
				t.Fatal("delivery retained artifact ownership", err)
			}
			noReportFiles(t, manager)
		})
	}
}

func TestExportHandlerRejectsResolverFailuresBeforeSQLAndDelivery(t *testing.T) {
	fixture := reportPostgres(t)
	manager, table := reportManager(t, fixture, reportSpec(), nil)
	for name, resolve := range map[string]func(context.Context, string) (reportActor, Request, ExportOptions, error){
		"error": func(context.Context, string) (reportActor, Request, ExportOptions, error) {
			return reportActor{}, Request{}, ExportOptions{}, errors.New("actor no longer exists")
		},
		"panic": func(context.Context, string) (reportActor, Request, ExportOptions, error) { panic("provider panic") },
		"Goexit": func(context.Context, string) (reportActor, Request, ExportOptions, error) {
			runtime.Goexit()
			return reportActor{}, Request{}, ExportOptions{}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			delivered := false
			handler, err := ExportHandler(table, manager, resolve, func(context.Context, string, *Artifact) error { delivered = true; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := handler(t.Context(), "actor reference"); err == nil || delivered {
				t.Fatal("failed resolver continued export", err)
			}
			noReportFiles(t, manager)
		})
	}
}

func TestExportHandlerDeliveryRetainsExportLifetimeContext(t *testing.T) {
	fixture := reportPostgres(t)
	t.Run("self shutdown", func(t *testing.T) {
		manager, table := reportManager(t, fixture, reportSpec(), nil)
		handler, err := ExportHandler(table, manager, func(context.Context, struct{}) (reportActor, Request, ExportOptions, error) {
			return reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV}, nil
		}, func(ctx context.Context, _ struct{}, _ *Artifact) error { return manager.Close(ctx) })
		if err != nil {
			t.Fatal(err)
		}
		// A failed regression is bounded instead of hanging on its own file.
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := handler(ctx, struct{}{}); !errors.Is(err, fault.Cycle) {
			t.Fatal("delivery lost its owned export frame", err)
		}
		if count, err := table.Count(t.Context(), manager, reportActor{Tenant: 7, Allowed: true}, Request{}); err != nil || count != 4 {
			t.Fatal("delivery self-close partially shut down the manager", err)
		}
		noReportFiles(t, manager)
	})
	t.Run("deadline and request values", func(t *testing.T) {
		manager, table := reportManager(t, fixture, reportSpec(), func(config *Config) { config.ExportTimeout = time.Second })
		type jobContextKey struct{}
		ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), jobContextKey{}, "execution identity"), 5*time.Second)
		defer cancel()
		parentDeadline, _ := ctx.Deadline()
		handler, err := ExportHandler(table, manager, func(context.Context, struct{}) (reportActor, Request, ExportOptions, error) {
			return reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV}, nil
		}, func(delivery context.Context, _ struct{}, artifact *Artifact) error {
			deadline, present := delivery.Deadline()
			if !present || !deadline.Before(parentDeadline) || delivery.Value(jobContextKey{}) != "execution identity" {
				return errors.New("delivery lost the export deadline or job context")
			}
			_, err := io.Copy(io.Discard, artifact)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := handler(ctx, struct{}{}); err != nil {
			t.Fatal(err)
		}
		noReportFiles(t, manager)
	})
	t.Run("cancellation cannot report success", func(t *testing.T) {
		manager, table := reportManager(t, fixture, reportSpec(), nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		handler, err := ExportHandler(table, manager, func(context.Context, struct{}) (reportActor, Request, ExportOptions, error) {
			return reportActor{Tenant: 7, Allowed: true}, Request{}, ExportOptions{Format: CSV}, nil
		}, func(context.Context, struct{}, *Artifact) error { cancel(); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if err := handler(ctx, struct{}{}); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled delivery reported success", err)
		}
		noReportFiles(t, manager)
	})
}
