package mfa

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
)

type enrollmentJSONOwner struct{}
type anotherEnrollmentJSONOwner struct{}

func TestEnrollmentIDJSONPreservesOwnerAndRejectsInvalidInput(t *testing.T) {
	raw, err := model.NewID[Record]()
	if err != nil {
		t.Fatal(err)
	}
	id, err := ParseEnrollmentID[enrollmentJSONOwner](raw.String())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := id.JSONContract()
	description, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	other, err := (EnrollmentID[anotherEnrollmentJSONOwner]{}).JSONContract().Description()
	if err != nil {
		t.Fatal(err)
	}
	if description.Root == other.Root || description.Types[0].Format != contract.UUIDFormat {
		t.Fatal("enrollment contract lost model owner or UUID shape")
	}
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var decoded EnrollmentID[enrollmentJSONOwner]
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != id {
		t.Fatal("enrollment ID round trip", err)
	}
	for _, input := range []string{`null`, `42`, `{}`, `""`, `"00000000-0000-0000-0000-000000000000"`, `"bad"`} {
		decoded = id
		if err := json.Unmarshal([]byte(input), &decoded); err == nil || !decoded.IsZero() {
			t.Fatal("invalid input retained enrollment", err)
		}
	}
	var absent *EnrollmentID[enrollmentJSONOwner]
	if absent.UnmarshalJSON(data) == nil {
		t.Fatal("nil destination accepted")
	}
	if _, err := json.Marshal(EnrollmentID[enrollmentJSONOwner]{}); err == nil {
		t.Fatal("empty enrollment serialized")
	}
}
