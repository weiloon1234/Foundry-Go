package clientcontracts

import (
	"context"
	"errors"

	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:dto
type CreateMember struct {
	Name string `json:"name"`
}

//foundry:dto
type MemberCreated struct {
	ID model.ID[models.User] `json:"id"`
}

//foundry:dto
type MemberExistsDetails struct {
	ExistingMemberID model.ID[models.User] `json:"existing_member_id"`
	Existing         MemberConflictState   `json:"existing"`
}

//foundry:dto
type MemberConflictState struct {
	Name  string              `json:"name"`
	State localization.Status `json:"state"`
}

var MemberExists = foundryhttp.DefineErrorDetails("member_exists", 409, "The member already exists.", MemberExistsDetailsJSON())
var RegistrationClosed = foundryhttp.DefineError("registration_closed", 409, "Registration is closed.")

var MemberCreate = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members")),
	foundryhttp.EmptyQuery(), foundryhttp.JSONBody(CreateMemberJSON()), foundryhttp.JSONResponse(201, MemberCreatedJSON()),
	foundryhttp.Errors(MemberExists, RegistrationClosed),
)

type existingMember struct {
	ID    model.ID[models.User]
	Name  string
	State localization.Status
}

func (*existingMember) Error() string { return "private-member-cause" }

var registrationClosed = errors.New("private-registration-cause")

// The fixture directory has two public sample entries and no external state.
// An application service returns its domain error, independent of HTTP and MCP.
func createMember(name string) (model.ID[models.User], error) {
	id, err := model.ParseID[models.User]("0193fd8c-2075-7000-8000-000000000002")
	if err != nil {
		return id, err
	}
	switch name {
	case "existing":
		return model.ID[models.User]{}, &existingMember{ID: id, Name: "Existing member", State: localization.Ready}
	case "closed":
		return model.ID[models.User]{}, registrationClosed
	default:
		return id, nil
	}
}

// HandleMemberCreate maps domain failures at the transport boundary.
// Example: MemberCreate.Handle(HandleMemberCreate).
func HandleMemberCreate(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, CreateMember]) (MemberCreated, error) {
	id, err := createMember(in.Body.Name)
	var existing *existingMember
	if errors.As(err, &existing) {
		// These fixture entries are public. Real handlers check current resource
		// authorization before exposing an existing member's identity or state.
		return MemberCreated{}, MemberExists.New(MemberExistsDetails{ExistingMemberID: existing.ID, Existing: MemberConflictState{Name: existing.Name, State: existing.State}}).WithCause(err)
	}
	if errors.Is(err, registrationClosed) {
		return MemberCreated{}, RegistrationClosed.WithCause(err)
	}
	return MemberCreated{ID: id}, err
}
