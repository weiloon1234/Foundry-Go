package nestedbindings

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:path pattern=/teams/{team}/projects/{project}/tasks/{task}
type Path struct {
	Team    model.ID[Team]
	Project Slug
	Task    Slug
}

//foundry:dto
type Reply struct {
	Team    string `json:"team"`
	Project Slug   `json:"project"`
	Task    Slug   `json:"task"`
}

type TeamProject = modelbinding.Models[Team, Project]
type Resources = modelbinding.Models[TeamProject, Task]
type Request = modelbinding.Input[Path, foundryhttp.NoQuery, foundryhttp.NoBody, Resources]
type TransportRequest = foundryhttp.Input[Path, foundryhttp.NoQuery, foundryhttp.NoBody]

func Transport(access foundryhttp.Access) foundryhttp.Endpoint[Path, foundryhttp.NoQuery, foundryhttp.NoBody, Reply] {
	return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "nested.show", Method: foundryhttp.GET, Access: access}, PathDescriptor()), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, ReplyJSON()))
}
func ProjectBySlug(db database.Executor) modelbinding.Resolver[Path, Project] {
	return modelbinding.ByColumn(db, ProjectRecords.Query().Where(orm.ColumnEq(ProjectRecords.Enabled, true)), orm.TextExpr(ProjectRecords.Slug), func(path Path) Slug { return path.Project })
}
func Projects(db database.Executor) modelbinding.Resolver[Path, TeamProject] {
	teams := modelbinding.ByKey(db, orm.Keyed(TeamRecords.Query(), TeamRecords.Key()), func(path Path) model.ID[Team] { return path.Team })
	return modelbinding.ThroughRelation(db, teams, TeamLinks.Projects.Where(orm.ColumnEq(ProjectRecords.Enabled, true)), orm.TextExpr(ProjectRecords.Slug), func(path Path) Slug { return path.Project })
}
func Resolve(db database.Executor) modelbinding.Resolver[Path, Resources] {
	return modelbinding.ThroughSelectedRelation(db, Projects(db), func(previous TeamProject) Project { return previous.Child }, ProjectLinks.Tasks, orm.TextExpr(TaskRecords.Slug), func(path Path) Slug { return path.Task })
}
func Handle(_ context.Context, input Request) (Reply, error) {
	return Reply{Team: input.Model.Parent.Parent.Name, Project: input.Model.Parent.Child.Slug, Task: input.Model.Child.Slug}, nil
}
