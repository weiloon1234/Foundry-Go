package attachments

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type rehearsalCapBackend struct{ storage.Backend }

func (b rehearsalCapBackend) Capabilities() storage.Capabilities {
	c := b.Backend.Capabilities()
	c.ConditionalWriteMaxBytes = 8
	return c.WithRehearsal(testDisk.ID(), deployrehearsal.New(map[storage.DiskID]int64{testDisk.ID(): 8}))
}

func TestPostgresRehearsalStartsWithLargePolicyAndSurfacesRuntimeRefusal(t *testing.T) {
	t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", strings.Repeat("a", 32))
	policy := largePolicy()
	policy.MaxBytes, policy.MaxStoredBytes = 500<<20, 500<<20
	collection := Define(extensiontest.Members, "rehearsal", policy)
	f := openAttachmentsOn(t, nil, func(b storage.Backend) storage.Backend { return rehearsalCapBackend{b} }, collection.Registration())
	_, err := collection.Add(t.Context(), f.manager, member(t, 1), Upload{Source: strings.NewReader("a small upload above the runtime cap")})
	var detail *storage.Error
	if !errors.Is(err, storage.RehearsalLimitExceeded) || !errors.As(err, &detail) || detail.Outcome() != storage.Unchanged || !strings.Contains(err.Error(), "reduced-fidelity rehearsal") {
		t.Fatal("attachment hid the rehearsal refusal", err)
	}
	if f.disk.RehearsalRefusals() != 1 {
		t.Fatal("runtime refusal was not counted")
	}
	policies := f.manager.RehearsalPolicies()[testDisk.ID()]
	found := false
	for _, p := range policies {
		found = found || p.MaxBytes == 500<<20
	}
	if !found {
		t.Fatal("declared large maximum missing")
	}
}
