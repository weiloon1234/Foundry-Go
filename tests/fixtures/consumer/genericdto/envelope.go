// Package genericdto exercises reusable typed response contracts in an
// independently compiled consumer. Framework infrastructure stays out of DTOs.
package genericdto

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type User struct{}
type Project struct{}

//foundry:dto http=body
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

// doc-example:begin generic-envelope

//foundry:dto
type Envelope[T any] struct {
	Data  T      `json:"data"`
	Trace string `json:"trace"`
}

// doc-example:end generic-envelope

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

var Echo = func() foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, UserDTO, Envelope[UserDTO]] {
	genericEchoRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "generic.echo", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/generic/echo"))
	return foundryhttp.DefineEndpoint(
		genericEchoRoute,
		foundryhttp.EmptyQuery(), UserDTOBody(), foundryhttp.JSONResponse(200, EnvelopeJSON(UserDTOJSON())),
	)
}()

var ShowProject = func() foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Envelope[ProjectDTO]] {
	genericProjectRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "generic.project", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/generic/project"))
	return foundryhttp.DefineEndpoint(
		genericProjectRoute,
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, EnvelopeJSON(ProjectDTOJSON())),
	)
}()

type EchoInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, UserDTO]

func HandleEcho(_ context.Context, input EchoInput) (Envelope[UserDTO], error) {
	return Envelope[UserDTO]{Data: input.Body, Trace: "fixture"}, nil
}

func HandleProject(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (Envelope[ProjectDTO], error) {
	id, err := model.ParseID[Project]("0193fd8c-2075-7000-8000-000000000002")
	return Envelope[ProjectDTO]{Data: ProjectDTO{ID: id, Title: "Typed project"}, Trace: "fixture"}, err
}

func Router() (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Echo.Handle(HandleEcho), ShowProject.Handle(HandleProject))
}

func DisplayName(in Envelope[UserDTO]) string { return in.Data.Name }

func ValidateUserEnvelope(rule validation.Rule[UserDTO]) validation.Rule[Envelope[UserDTO]] {
	return validation.Rules(EnvelopeValidationFields[UserDTO]().Data, rule)
}
