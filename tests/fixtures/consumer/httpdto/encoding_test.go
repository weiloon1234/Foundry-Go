package httpdto_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestGeneratedResponsesEncodeAsTypedRecordsListsAndNulls(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	response := httpdto.UserResponse{ID: id, Email: "member@example.test", State: models.StatusActive}
	rows := contract.Slice(httpdto.UserResponseJSON())
	data, err := rows.Encode(context.Background(), []httpdto.UserResponse{response}, limits())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := rows.Decode(context.Background(), data, limits())
	if err != nil || len(decoded) != 1 || decoded[0] != response {
		t.Fatalf("typed response list changed: %v", err)
	}
	optional := contract.Nullable(httpdto.UserResponseJSON())
	data, err = optional.Encode(context.Background(), value.Of(response), limits())
	if err != nil {
		t.Fatal(err)
	}
	record, err := optional.Decode(context.Background(), data, limits())
	present, set := record.Get()
	if err != nil || !set || present != response {
		t.Fatalf("nullable response changed: %v", err)
	}
	data, err = optional.Encode(context.Background(), value.Null[httpdto.UserResponse](), limits())
	if err != nil || string(data) != "null" {
		t.Fatalf("explicit null response changed: %s %v", data, err)
	}
	nullableRows := contract.Slice(optional)
	data, err = nullableRows.Encode(context.Background(), []value.Nullable[httpdto.UserResponse]{value.Null[httpdto.UserResponse](), value.Of(response)}, limits())
	if err != nil {
		t.Fatal(err)
	}
	values, err := nullableRows.Decode(context.Background(), data, limits())
	if err != nil || len(values) != 2 || !values[0].IsNull() || values[1].IsNull() {
		t.Fatalf("nullable list members changed: %v", err)
	}
}

func TestGeneratedEncodingRejectsInvalidModelsWithoutPartialBody(t *testing.T) {
	response := httpdto.UserResponse{Email: "private@example.test", State: models.Status("undeclared")}
	data, err := httpdto.UserResponseJSON().Encode(context.Background(), response, limits())
	var failure *contract.EncodeError
	if data != nil || !errors.As(err, &failure) {
		t.Fatalf("invalid response exposed a partial body: %v", err)
	}
}
