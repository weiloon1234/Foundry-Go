package attachments

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type policyCapBackend struct {
	storage.Backend
	capabilities storage.Capabilities
}

func (b policyCapBackend) Capabilities() storage.Capabilities { return b.capabilities }

func TestRehearsalManagerAcceptsPolicyAboveCapWithoutDatabaseIO(t *testing.T) {
	t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", strings.Repeat("a", 32))
	owners, err := extensions.NewRegistry(extensiontest.Members.Registration())
	if err != nil {
		t.Fatal(err)
	}
	// Construction borrows the database; no connection is needed or started.
	store, err := extensions.New(&database.DB{}, owners, extensions.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(t.Context())
	policy := largePolicy()
	policy.MaxBytes, policy.MaxStoredBytes = 500<<20, 500<<20
	collection := Define(extensiontest.Members, "rehearsal", policy)
	for _, mode := range []storage.ConditionalWriteMode{"", storage.RehearsalRuntime} {
		caps := storage.Capabilities{ConditionalCreate: true, ConditionalRead: true, ConditionalWriteMaxBytes: 8, ConditionalWriteMode: mode}
		disk, err := testDisk.Bind(policyCapBackend{capabilities: caps}, storage.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		defer disk.Close(t.Context())
		disks, err := storage.NewRegistry(disk)
		if err != nil {
			t.Fatal(err)
		}
		manager, err := New(Dependencies{Store: store, Disks: disks}, DefaultConfig(), collection.Registration())
		if mode == "" {
			if !errors.Is(err, storage.Unsupported) {
				t.Fatal("strict startup changed", err)
			}
			continue
		}
		if err != nil {
			t.Fatal("rehearsal startup refused policy", err)
		}
		defer manager.Close(t.Context())
		maxima := manager.RehearsalPolicies()[testDisk.ID()]
		if len(maxima) != 1 || maxima[0].MaxBytes != policy.MaxStoredBytes {
			t.Fatal("declared maximum missing", maxima)
		}
		maxima[0].MaxBytes = 0
		if manager.RehearsalPolicies()[testDisk.ID()][0].MaxBytes != policy.MaxStoredBytes {
			t.Fatal("inspection returned borrowed state")
		}
		key, _ := storage.ParseKey("private-object")
		_, err = disk.Put(t.Context(), key, strings.NewReader("private-body"), storage.PutOptions{Size: value.Set[int64](9), Condition: storage.IfAbsent()})
		err = joinFailures(err)
		if !errors.Is(err, storage.RehearsalLimitExceeded) || !strings.Contains(err.Error(), "reduced-fidelity rehearsal") {
			t.Fatal("attachment wrapping hid cap refusal", err)
		}
		// A normal object limit remains a startup requirement even in rehearsal.
		tooLarge := policy
		tooLarge.MaxBytes, tooLarge.MaxStoredBytes = 2<<30, 2<<30
		if _, err := New(Dependencies{Store: store, Disks: disks}, DefaultConfig(), Define(extensiontest.Members, "too-large", tooLarge).Registration()); err == nil {
			t.Fatal("rehearsal bypassed MaxObjectBytes")
		}
	}
}
