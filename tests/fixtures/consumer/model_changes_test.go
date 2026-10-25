package consumer_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestORMModelChangesKeepOnlyPersistedSnapshots(t *testing.T) {
	before := models.User{
		ID: model.IDFromBytes[models.User]([16]byte{1}), Email: "private-before", Status: models.StatusActive, Level: models.LevelBasic,
		Scratch: []string{"transient secret"}, Referrals: relation.Collection([]models.User{}), Introducer: relation.Single(value.Optional[models.User]{}),
	}
	after := before
	after.Email = "private-after"
	after.Nickname = value.Of("")
	changes, err := orm.Compare(models.Users.Table, value.Set(before), value.Set(after), models.Users.Email.Set("input transformed elsewhere"), models.Users.Nickname.Set(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []value.Optional[models.User]{changes.Before(), changes.After()} {
		user, present := snapshot.Get()
		if !present || user.Scratch != nil || user.Referrals.IsLoaded() || user.Introducer.IsLoaded() || user.ID != before.ID {
			t.Fatal("snapshot retained ignored/relation state or lost persisted identity")
		}
	}
	emailChange, err := orm.ChangeOf(changes, models.Users.Email)
	if err != nil {
		t.Fatal(err)
	}
	idChange, err := orm.ChangeOf(changes, models.Users.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Changed() || !changes.Assigned() || !emailChange.Changed() || idChange.Changed() || idChange.Assigned() {
		t.Fatal("model-wide field changes lost assignment or identity state")
	}
	if email, present := emailChange.After().Get(); !present || email != "private-after" {
		t.Fatal("change set used draft input instead of stored result")
	}
	fieldCopy := emailChange
	fieldCopy = lifecycle.FieldChange[string]{}
	reloaded, err := orm.ChangeOf(changes, models.Users.Email)
	if err != nil || fieldCopy.Changed() || !reloaded.Changed() {
		t.Fatal("editing returned field data changed its source")
	}
	if before.Scratch[0] != "transient secret" || !before.Referrals.IsLoaded() || !before.Introducer.IsLoaded() {
		t.Fatal("snapshot extraction changed input models")
	}
	for _, formatted := range []string{fmt.Sprint(changes), fmt.Sprintf("%#v", changes), fmt.Sprint(emailChange), fmt.Sprintf("%#v", emailChange)} {
		if strings.Contains(formatted, "private-") || strings.Contains(formatted, "transient secret") {
			t.Fatal("change diagnostics exposed snapshot data")
		}
	}
}

func TestORMModelChangesRejectMixedIdentityAndInvalidSnapshots(t *testing.T) {
	before := models.User{ID: model.IDFromBytes[models.User]([16]byte{1}), Status: models.StatusActive, Level: models.LevelBasic}
	other := before
	other.ID = model.IDFromBytes[models.User]([16]byte{2})
	invalid := before
	invalid.Status = "invalid"
	for _, test := range []struct {
		before, after value.Optional[models.User]
		draft         []orm.Assignment[models.User]
	}{
		{value.Set(before), value.Set(other), nil},
		{value.Set(before), value.Set(before), []orm.Assignment[models.User]{models.Users.ID.Set(before.ID)}},
		{value.Set(before), value.Optional[models.User]{}, []orm.Assignment[models.User]{models.Users.Email.Set("invalid deletion")}},
		{value.Optional[models.User]{}, value.Optional[models.User]{}, nil},
		{value.Set(before), value.Set(invalid), nil},
		{value.Set(invalid), value.Set(before), nil},
	} {
		changes, err := orm.Compare(models.Users.Table, test.before, test.after, test.draft...)
		if !errors.Is(err, fault.Invalid) || changes.Before().IsSet() || changes.After().IsSet() || changes.Changed() || changes.Assigned() {
			t.Fatalf("invalid model comparison published partial snapshots: %v", err)
		}
	}
}
