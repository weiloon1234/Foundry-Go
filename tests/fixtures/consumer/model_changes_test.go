package consumer_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestGeneratedModelChangesKeepOnlyPersistedSnapshots(t *testing.T) {
	before := models.User{
		ID: model.IDFromBytes[models.User]([16]byte{1}), Email: "private-before", Status: models.StatusActive, Level: models.LevelBasic,
		Scratch: []string{"transient secret"}, Referrals: relation.Collection([]models.User{}), Introducer: relation.Single(value.Optional[models.User]{}),
	}
	after := before
	after.Email = "private-after"
	after.Nickname = value.Of("")
	changes, err := models.CompareUser(value.Set(before), value.Set(after), models.UserDraft{}.SetEmail("input transformed elsewhere").SetNickname(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []value.Optional[models.User]{changes.Before(), changes.After()} {
		user, present := snapshot.Get()
		if !present || user.Scratch != nil || user.Referrals.IsLoaded() || user.Introducer.IsLoaded() || user.ID != before.ID {
			t.Fatal("snapshot retained ignored/relation state or lost persisted identity")
		}
	}
	if !changes.Changed() || !changes.Assigned() || !changes.Fields().Email.Changed() || changes.Fields().ID.Changed() || changes.Fields().ID.Assigned() {
		t.Fatal("model-wide field changes lost assignment or identity state")
	}
	if email, present := changes.Fields().Email.After().Get(); !present || email != "private-after" {
		t.Fatal("change set used draft input instead of stored result")
	}
	fields := changes.Fields()
	fields.Email = models.UserChanges{}.Fields().Email
	if !changes.Fields().Email.Changed() {
		t.Fatal("editing returned field data changed its source")
	}
	if before.Scratch[0] != "transient secret" || !before.Referrals.IsLoaded() || !before.Introducer.IsLoaded() {
		t.Fatal("snapshot extraction changed input models")
	}
	for _, formatted := range []string{fmt.Sprint(changes), fmt.Sprintf("%#v", changes), fmt.Sprint(changes.Fields()), fmt.Sprintf("%#v", changes.Fields())} {
		if strings.Contains(formatted, "private-") || strings.Contains(formatted, "transient secret") {
			t.Fatal("change diagnostics exposed snapshot data")
		}
	}
}

func TestGeneratedModelChangesRejectMixedIdentityAndInvalidSnapshots(t *testing.T) {
	before := models.User{ID: model.IDFromBytes[models.User]([16]byte{1}), Status: models.StatusActive, Level: models.LevelBasic}
	other := before
	other.ID = model.IDFromBytes[models.User]([16]byte{2})
	invalid := before
	invalid.Status = "invalid"
	for _, test := range []struct {
		before, after value.Optional[models.User]
		draft         models.UserDraft
	}{
		{value.Set(before), value.Set(other), models.UserDraft{}},
		{value.Set(before), value.Set(before), models.UserDraft{}.SetID(before.ID)},
		{value.Set(before), value.Optional[models.User]{}, models.UserDraft{}.SetEmail("invalid deletion")},
		{value.Optional[models.User]{}, value.Optional[models.User]{}, models.UserDraft{}},
		{value.Set(before), value.Set(invalid), models.UserDraft{}},
		{value.Set(invalid), value.Set(before), models.UserDraft{}},
	} {
		changes, err := models.CompareUser(test.before, test.after, test.draft)
		if !errors.Is(err, fault.Invalid) || changes.Before().IsSet() || changes.After().IsSet() || changes.Changed() || changes.Assigned() {
			t.Fatalf("invalid model comparison published partial snapshots: %v", err)
		}
	}
}
