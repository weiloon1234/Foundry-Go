package httpdto_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
)

func limits() contract.JSONLimits {
	return contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
}

func TestGeneratedRequestUsesConcretePatchStates(t *testing.T) {
	declaration := httpdto.UpdateUserJSON()
	input, err := declaration.Decode(context.Background(), []byte(`{"nickname":null,"state":"active"}`), limits())
	if err != nil {
		t.Fatal(err)
	}
	if input.Email.IsSet() {
		t.Fatal("omitted email became a write")
	}
	nickname, set := input.Nickname.Get()
	if !set || !nickname.IsNull() {
		t.Fatal("explicit nickname clear was lost")
	}
	state, set := input.State.Get()
	if !set || state != models.StatusActive {
		t.Fatal("imported enum was not retained")
	}
	for _, data := range []string{`{"email":null}`, `{"state":"undeclared"}`, `{"Email":"private@example.test"}`, `{"nickname":null,"nickname":"changed"}`} {
		_, err := declaration.Decode(context.Background(), []byte(data), limits())
		var failure *contract.DecodeError
		if !errors.As(err, &failure) {
			t.Fatalf("invalid request accepted: %v", err)
		}
	}
}

func TestGeneratedResponseRetainsItsModelIDOwner(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(httpdto.UserResponse{ID: id, Email: "member@example.test", State: models.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	declaration := httpdto.UserResponseJSON()
	response, err := declaration.Decode(context.Background(), data, limits())
	if err != nil || response.ID != id || response.State != models.StatusActive {
		t.Fatalf("response lost concrete types: %v", err)
	}
	description, err := declaration.Description()
	if err != nil {
		t.Fatal(err)
	}
	if description.Root != "foundry.test/consumer/httpdto.UserResponse" {
		t.Fatal("response identity does not match its Go declaration")
	}
	for _, typ := range description.Types {
		for _, property := range typ.Properties {
			if property.Name == "password" {
				t.Fatal("persistence fields leaked into the response contract")
			}
		}
	}
}
