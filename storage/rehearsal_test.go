package storage_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/value"
)

type rehearsalBackend struct {
	storage.Backend
	grant storage.Rehearsal
}

func (b rehearsalBackend) Capabilities() storage.Capabilities {
	c := b.Backend.Capabilities()
	c.ConditionalWriteMaxBytes = 8
	return c.WithRehearsal("streams", b.grant)
}

type unreadRehearsalSource struct{ reads int }

func (r *unreadRehearsalSource) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }

func TestRehearsalRuntimeCap(t *testing.T) {
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	disk := streamDisk(t, rehearsalBackend{backend, deployrehearsal.New(map[storage.DiskID]int64{"streams": 8})}, func(*storage.Config) {})
	caps := disk.Capabilities()
	policy := storage.PutOptions{Size: value.Set[int64](500 << 20), Condition: storage.IfAbsent()}
	if err := caps.ValidatePutPolicy(policy); err != nil {
		t.Fatal("rehearsal startup policy refused", err)
	}
	forged := storage.Capabilities{ConditionalCreate: true, ConditionalWriteMaxBytes: 8, ConditionalWriteMode: storage.RehearsalRuntime}
	if err := forged.ValidatePutPolicy(policy); !errors.Is(err, storage.Invalid) {
		t.Fatal("unsealed mode bypass", err)
	}
	caps.ConditionalWriteMode = ""
	if err := caps.ValidatePutPolicy(policy); !errors.Is(err, storage.Unsupported) {
		t.Fatal("strict startup changed", err)
	}
	key := testKey(t)
	for _, condition := range []storage.WriteCondition{storage.IfAbsent(), mustMatchRehearsal(t)} {
		for _, size := range []value.Optional[int64]{value.Set[int64](9), {}} {
			source := &unreadRehearsalSource{}
			_, err := disk.Put(t.Context(), key, source, storage.PutOptions{Size: size, Condition: condition})
			var detail *storage.Error
			if !errors.Is(err, storage.RehearsalLimitExceeded) || !errors.As(err, &detail) || detail.Outcome() != storage.Unchanged || source.reads != 0 || !strings.Contains(err.Error(), "reduced-fidelity rehearsal") {
				t.Fatal("cap did not refuse before reading with a typed unchanged result", err)
			}
		}
	}
	if disk.RehearsalRefusals() != 4 {
		t.Fatal("refusal count", disk.RehearsalRefusals())
	}
	if _, err := disk.Put(t.Context(), key, strings.NewReader("12345678"), storage.PutOptions{Size: value.Set[int64](8), Condition: storage.IfAbsent()}); err != nil {
		t.Fatal("at-cap write", err)
	}
	if _, err := disk.Put(t.Context(), key, strings.NewReader("123456789"), storage.PutOptions{Size: value.Set[int64](9)}); err != nil {
		t.Fatal("unconditional write", err)
	}
	if disk.RehearsalRefusals() != 4 {
		t.Fatal("successful writes counted as refusals")
	}
	if _, err := storage.NewDisk("production", rehearsalBackend{backend, deployrehearsal.New(map[storage.DiskID]int64{"streams": 8})}, storage.DefaultConfig()); !errors.Is(err, storage.Invalid) {
		t.Fatal("grant reused for another disk", err)
	}
}

func mustMatchRehearsal(t *testing.T) storage.WriteCondition {
	t.Helper()
	condition, err := storage.IfMatch(`"old"`)
	if err != nil {
		t.Fatal(err)
	}
	return condition
}
