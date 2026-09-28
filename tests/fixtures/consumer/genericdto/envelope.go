// Package genericdto exercises reusable typed response contracts in an
// independently compiled consumer. Framework infrastructure stays out of DTOs.
package genericdto

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type User struct{}
type Project struct{}

//foundry:dto
type UserDTO struct {
	ID       model.ID[User]                         `json:"id"`
	Name     string                                 `json:"name"`
	Note     value.Optional[value.Nullable[string]] `json:"note,omitzero"`
	Sequence int64                                  `json:"sequence"`
}

//foundry:dto
type ProjectDTO struct {
	ID    model.ID[Project] `json:"id"`
	Title string            `json:"title"`
}

//foundry:dto
type Envelope[T any] struct {
	Data  T      `json:"data"`
	Trace string `json:"trace"`
}

//foundry:dto
type UserEnvelope struct {
	Data  UserDTO `json:"data"`
	Trace string  `json:"trace"`
}

//foundry:dto
type Combined struct {
	User    Envelope[UserDTO]    `json:"user"`
	Project Envelope[ProjectDTO] `json:"project"`
}

var Echo = http.DefineEndpoint(
	http.DefineRoute(http.RouteSpec{ID: "generic.echo", Method: http.POST, Access: http.Public}, http.StaticPath("/generic/echo")),
	http.EmptyQuery(), http.JSONBody(UserDTOJSON()), http.JSONResponse(200, EnvelopeJSON(UserDTOJSON())),
)

var ShowProject = http.DefineEndpoint(
	http.DefineRoute(http.RouteSpec{ID: "generic.project", Method: http.GET, Access: http.Public}, http.StaticPath("/generic/project")),
	http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, EnvelopeJSON(ProjectDTOJSON())),
)

type EchoInput = http.Input[http.NoPath, http.NoQuery, UserDTO]

func HandleEcho(_ context.Context, input EchoInput) (Envelope[UserDTO], error) {
	return Envelope[UserDTO]{Data: input.Body, Trace: "fixture"}, nil
}

func HandleProject(context.Context, http.Input[http.NoPath, http.NoQuery, http.NoBody]) (Envelope[ProjectDTO], error) {
	id, err := model.ParseID[Project]("0193fd8c-2075-7000-8000-000000000002")
	return Envelope[ProjectDTO]{Data: ProjectDTO{ID: id, Title: "Typed project"}, Trace: "fixture"}, err
}

func Router() (*http.Router, error) {
	return http.NewRouter(Echo.Handle(HandleEcho), ShowProject.Handle(HandleProject))
}

func DisplayName(in Envelope[UserDTO]) string { return in.Data.Name }

func ValidateUserEnvelope(rule validation.Rule[UserDTO]) validation.Rule[Envelope[UserDTO]] {
	return EnvelopeValidationFields[UserDTO]().Data.Rules(rule)
}
