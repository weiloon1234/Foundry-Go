package mutatorqueries_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// This DTO belongs to the consumer. Persistence models do not implicitly
// choose which fields or accessor values a transport should expose.
type memberResponse struct {
	ID       string                                         `json:"id"`
	Email    mutatorqueries.DisplayEmail                    `json:"email"`
	Nickname value.Nullable[mutatorqueries.DisplayNickname] `json:"nickname"`
}

func responseForMember(member mutatorqueries.Member) (memberResponse, error) {
	id, err := member.AccessID()
	if err != nil {
		return memberResponse{}, err
	}
	email, err := member.AccessEmail()
	if err != nil {
		return memberResponse{}, err
	}
	nickname, err := member.AccessNickname()
	if err != nil {
		return memberResponse{}, err
	}
	return memberResponse{ID: id, Email: email, Nickname: nickname}, nil
}

func TestTypedModelAccessorsPreserveStoredFields(t *testing.T) {
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "ada@example.test", Nickname: value.Of("ada")}
	stored := member
	if member.ID != id || member.Attempts != 0 {
		t.Fatal("ordinary field reads changed identity or invoked a setter")
	}
	storedEmail := member.Email
	if storedEmail != "ada@example.test" {
		t.Fatal("field access did not retain the stored value")
	}
	response, err := responseForMember(member)
	if err != nil || response.Email != "ADA@EXAMPLE.TEST" || response.ID != id.String() || response.Nickname != value.Of(mutatorqueries.DisplayNickname("ADA")) {
		t.Fatalf("typed getter DTO: %+v, %v", response, err)
	}
	if member != stored {
		t.Fatal("getter evaluation changed stored fields")
	}
	encoded, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(encoded), `"email":"ADA@EXAMPLE.TEST"`) {
		t.Fatalf("DTO did not encode its explicit getter result: %s, %v", encoded, err)
	}
	member.Email = "  RAW@EXAMPLE.TEST  "
	if member.Email != "  RAW@EXAMPLE.TEST  " {
		t.Fatal("ordinary assignment unexpectedly invoked a setter")
	}
	member.Email = "hidden@example.test"
	if response, err := responseForMember(member); !errors.Is(err, mutatorqueries.HiddenEmail) || response != (memberResponse{}) {
		t.Fatalf("getter failure exposed a partial DTO: %+v, %v", response, err)
	}
	for _, nickname := range []value.Nullable[string]{value.Null[string](), value.Of("")} {
		member.Nickname = nickname
		accessed, err := member.AccessNickname()
		if err != nil || accessed.IsNull() != nickname.IsNull() || member.Nickname != nickname {
			t.Fatalf("getter lost NULL/empty distinction: %v", err)
		}
	}
}
