package checks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/health/checks"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

func TestDiskProbeStatsWithoutWritingAndMailerProbeTracksAdmission(t *testing.T) {
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.DefineDisk("readiness").Bind(backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := disk.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	diskProbe, err := checks.Disk("storage.readiness", disk, checks.DiskProbeKey())
	if err != nil {
		t.Fatal(err)
	}
	mailer, err := email.New(email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) { return email.Receipt{}, nil }), nil, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mailProbe, err := checks.Mailer("mail.default", mailer)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := health.NewRegistry(health.DefaultConfig(), diskProbe, mailProbe)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close(context.Background())
	report, err := registry.Check(t.Context())
	if err != nil || !report.Ready {
		t.Fatal("reachable disk or open mailer was not ready", err, report)
	}
	if exists, err := disk.Exists(t.Context(), checks.DiskProbeKey()); err != nil || exists {
		t.Fatal("disk probe wrote its probe key", exists, err)
	}
	if err := mailer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	report, err = registry.Check(t.Context())
	if err != nil || report.Ready || report.Results[0].State != health.Up || report.Results[1].State != health.Down {
		t.Fatal("closing mailer did not fail its probe", err, report)
	}
	if _, err := checks.Disk("storage.none", nil, checks.DiskProbeKey()); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing disk accepted")
	}
	if _, err := checks.Mailer("mail.none", nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing mailer accepted")
	}
}

// statBackend answers every stat with one configured failure.
type statBackend struct {
	storage.Backend
	failure storage.Code
}

func (b statBackend) Capabilities() storage.Capabilities { return storage.Capabilities{} }
func (b statBackend) Stat(context.Context, storage.ObjectKey, storage.ReadOptions) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.Failure(b.failure, storage.StatOperation, storage.NotApplicable, nil)
}

// S3 answers 403 for a missing key without s3:ListBucket; the endpoint and
// bucket answered, so the disk is reachable. Other failures stay Down.
func TestDiskProbeTreatsForbiddenAsAnswered(t *testing.T) {
	for code, ready := range map[storage.Code]bool{storage.Forbidden: true, storage.Unavailable: false} {
		disk, err := storage.DefineDisk("remote").Bind(statBackend{failure: code}, storage.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		probe, err := checks.Disk("storage.remote", disk, checks.DiskProbeKey())
		if err != nil {
			t.Fatal(err)
		}
		if err := probe.Check(t.Context()); (err == nil) != ready {
			t.Fatal("disk probe classification", code, err)
		}
	}
}
