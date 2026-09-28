package consumer_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type User struct{ ID model.ID[User] }
type ProfilePatch struct {
	Birthday value.Optional[value.Nullable[temporal.Date]] `json:"birthday,omitzero"`
}

func TestPublicValuesPreserveOwnershipAndPatchMeaning(t *testing.T) {
	id, err := model.NewID[User]()
	if err != nil {
		t.Fatal(err)
	}
	user := User{ID: id}
	if user.ID.IsZero() {
		t.Fatal("generated identity is zero")
	}
	date, err := temporal.ParseDate("2000-02-29")
	if err != nil {
		t.Fatal(err)
	}
	patch := ProfilePatch{Birthday: value.Set(value.Of(date))}
	data, err := json.Marshal(patch)
	if err != nil || string(data) != `{"birthday":"2000-02-29"}` {
		t.Fatalf("typed patch: %s %v", data, err)
	}
	if err := json.Unmarshal([]byte(`{"birthday":null}`), &patch); err != nil {
		t.Fatal(err)
	}
	birthday, present := patch.Birthday.Get()
	if !present || !birthday.IsNull() {
		t.Fatal("null became missing")
	}
}
