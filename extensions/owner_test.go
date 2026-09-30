package extensions_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestOwnerScopeFollowsTheDeclaredStorageModel(t *testing.T) {
	original := extensions.DefineOwner("members", extensiontest.MemberIdentity("extension_members"))
	renamed := extensions.DefineOwner("members", extensiontest.MemberIdentity("extension_people"))
	pinned := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "extension_members"})
	if original.StorageModel() != "extension_members" || pinned.StorageModel() != "extension_members" || pinned.ModelName() != "extension_people" {
		t.Fatal("storage model", pinned.StorageModel(), pinned.ModelName())
	}
	if renamed.Scope() == original.Scope() {
		t.Fatal("an undeclared rename kept the table-derived scope")
	}
	if pinned.Scope() != original.Scope() {
		t.Fatal("a declared storage model must keep the persisted scope across a rename")
	}
	// Identities persisted before the rename are attributed to the old table.
	stored, err := original.Reference(7).Identity()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pinned.Parse(stored)
	if err != nil || ref.Key() != 7 || ref.ModelName() != "extension_people" {
		t.Fatal("pinned owner rejected a pre-rename identity", err)
	}
	if _, err := renamed.Parse(stored); err == nil {
		t.Fatal("an undeclared model attribution was accepted")
	}
	before, err := original.Subject(original.Reference(7))
	if err != nil {
		t.Fatal(err)
	}
	after, err := pinned.Subject(ref)
	if err != nil || after.Scope != before.Scope || after.Key != before.Key {
		t.Fatal("pinned subject changed identity", err)
	}
	if err := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "bad name"}).Validate(); err == nil {
		t.Fatal("invalid storage model accepted")
	}
}

func TestRegistryRejectsStorageModelCollisionsAndAdoptsRenamedIdentities(t *testing.T) {
	pinned := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "extension_others"})
	if _, err := extensions.NewRegistry(extensiontest.Others.Registration(), pinned.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("storage model shadowed another owner's model", err)
	}
	if _, err := extensions.NewRegistry(pinned.Registration(), extensiontest.Others.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("storage model collision depends on registration order", err)
	}
	legacy, err := extensiontest.Members.Reference(3).Identity()
	if err != nil {
		t.Fatal(err)
	}
	// Re-scoping adopts only models the owner declares.
	undeclared := extensions.DefineOwner("members", extensiontest.MemberIdentity("extension_people"))
	strict, err := extensions.NewRegistry(undeclared.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.AdoptSubject("members", legacy); !errors.Is(err, fault.Invalid) || strict.DeclaresModel("members", "extension_members") {
		t.Fatal("re-scope adopted an undeclared earlier model", err)
	}
	renamed := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{PreviousModels: []string{"extension_members"}})
	if renamed.Scope() != undeclared.Scope() {
		t.Fatal("previous models changed the current scope")
	}
	registry, err := extensions.NewRegistry(renamed.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Subject("members", legacy); err == nil {
		t.Fatal("ordinary restoration accepted a previous model name")
	}
	if !registry.DeclaresModel("members", "extension_members") || !registry.DeclaresModel("members", "extension_people") || registry.DeclaresModel("members", "extension_others") || registry.DeclaresModel("others", "extension_members") {
		t.Fatal("declared model names")
	}
	subject, err := registry.AdoptSubject("members", legacy)
	if err != nil || subject.Scope != renamed.Scope() {
		t.Fatal("re-scope boundary", err)
	}
	expected, err := renamed.Subject(renamed.Reference(3))
	if err != nil || subject.Key != expected.Key {
		t.Fatal("adopted subject key", err)
	}
	if _, err := registry.AdoptSubject("unknown", legacy); err == nil {
		t.Fatal("unknown owner adopted")
	}
	for name, options := range map[string]extensions.OwnerOptions{
		"invalid":   {PreviousModels: []string{"bad name"}},
		"current":   {PreviousModels: []string{"extension_people"}},
		"storage":   {StorageModel: "extension_archive", PreviousModels: []string{"extension_archive"}},
		"repeated":  {PreviousModels: []string{"extension_members", "extension_members"}},
		"unbounded": {PreviousModels: make([]string, 17)},
	} {
		if err := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), options).Validate(); err == nil {
			t.Fatal("invalid previous models accepted", name)
		}
	}
	claimed := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{PreviousModels: []string{"extension_others"}})
	if _, err := extensions.NewRegistry(extensiontest.Others.Registration(), claimed.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("previous model shadowed another owner's model", err)
	}
	// The declaration snapshots caller-owned options.
	previous := []string{"extension_members"}
	snapshot := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{PreviousModels: previous})
	previous[0] = "extension_others"
	if models := snapshot.RecordedModels(); len(models) != 2 || models[1] != "extension_members" {
		t.Fatal("previous models were not snapshotted", models)
	}
}

// SubjectKey is Subject's key without the identity snapshot, through both the
// typed owner and the erased registry, including pre-rename identities.
func TestSubjectKeyMatchesSubject(t *testing.T) {
	pinned := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "extension_members"})
	registry, err := extensions.NewRegistry(pinned.Registration(), extensiontest.Others.Registration())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := extensiontest.Members.Reference(7).Identity()
	if err != nil {
		t.Fatal(err)
	}
	current, err := pinned.Reference(7).Identity()
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []model.Identity{legacy, current} {
		ref, err := pinned.Parse(identity)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := pinned.Subject(ref)
		if err != nil {
			t.Fatal(err)
		}
		key, err := pinned.SubjectKey(ref)
		if err != nil || key != subject.Key {
			t.Fatal("owner subject key differs from its subject", err)
		}
		erased, err := registry.Subject("members", identity)
		if err != nil {
			t.Fatal(err)
		}
		key, err = registry.SubjectKey("members", identity)
		if err != nil || key != erased.Key || key != subject.Key {
			t.Fatal("registry subject key differs from its subject", err)
		}
	}
	if _, err := registry.SubjectKey("unknown", current); !errors.Is(err, fault.Invalid) {
		t.Fatal("unknown owner", err)
	}
	if _, err := registry.SubjectKey("others", current); err == nil {
		t.Fatal("another owner's identity was accepted")
	}
	if _, err := pinned.SubjectKey(model.Reference[extensiontest.Member, int64]{}); err == nil {
		t.Fatal("a zero reference was accepted")
	}
}

func TestStoreDefaultsQueueBurstsWithHigherCapacity(t *testing.T) {
	config := extensions.DefaultConfig()
	if config.MaxActive != 64 || config.Timeout != time.Minute {
		t.Fatal("default admission", config.MaxActive, config.Timeout)
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.MaxActive = 1025
	if config.Validate() == nil {
		t.Fatal("unbounded admission accepted")
	}
}
