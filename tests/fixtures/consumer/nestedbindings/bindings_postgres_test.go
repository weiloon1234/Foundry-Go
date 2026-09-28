package nestedbindings_test

import (
	"context"
	"errors"
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixture struct {
	teams    []n.Team
	projects []n.Project
	tasks    []n.Task
}

func seed(ctx context.Context, tx *database.Tx) (fixture, error) {
	var f fixture
	for _, sql := range []string{`CREATE TABLE teams(id uuid PRIMARY KEY,name text NOT NULL)`, `CREATE TABLE projects(id uuid PRIMARY KEY,team_id uuid NOT NULL,slug text NOT NULL,enabled boolean NOT NULL,deleted_at timestamptz)`, `CREATE TABLE tasks(id uuid PRIMARY KEY,project_id uuid NOT NULL,slug text NOT NULL)`} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return f, err
		}
	}
	for _, name := range []string{"Alpha", "Beta"} {
		team, err := n.QueryTeams().Create(ctx, tx, n.TeamDraft{}.SetName(name))
		if err != nil {
			return f, err
		}
		f.teams = append(f.teams, team)
		project, err := n.QueryProjects().Create(ctx, tx, n.ProjectDraft{}.SetTeamID(team.ID).SetSlug("shared").SetEnabled(true))
		if err != nil {
			return f, err
		}
		f.projects = append(f.projects, project)
		task, err := n.QueryTasks().Create(ctx, tx, n.TaskDraft{}.SetProjectID(project.ID).SetSlug("same"))
		if err != nil {
			return f, err
		}
		f.tasks = append(f.tasks, task)
	}
	return f, nil
}
func route(t *testing.T, h http.Handler, ctx context.Context, location, token string, status int) {
	t.Helper()
	req := httptest.NewRequest("GET", location, nil).WithContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != status {
		t.Fatalf("%s: %d %s", location, res.Code, res.Body.String())
	}
}
func TestPostgresNestedBindingScopesUniquenessAndHooks(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		ctx := t.Context()
		f, err := seed(ctx, tx)
		if err != nil {
			return err
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		handled := 0
		endpoint := n.Transport(foundryhttp.Public)
		bound := modelbinding.Bind(endpoint, n.Resolve(counter)).WithAuthorization(func(_ context.Context, in n.Request) error {
			if in.Model.Parent.Parent.Name == "Beta" {
				return foundryhttp.Forbidden
			}
			return nil
		})
		router, err := foundryhttp.NewRouter(bound.Handle(func(ctx context.Context, in n.Request) (n.Reply, error) {
			handled++
			if in.Model.Child.ProjectID != in.Model.Parent.Child.ID || in.Model.Parent.Child.TeamID != in.Model.Parent.Parent.ID {
				t.Error("lost concrete nested ownership")
			}
			return n.Handle(ctx, in)
		}))
		if err != nil {
			return err
		}
		location := func(team model.ID[n.Team], project, task n.Slug) string {
			url, err := endpoint.URL(ctx, n.Path{Team: team, Project: project, Task: task}, foundryhttp.NoQuery{})
			if err != nil {
				t.Fatal(err)
			}
			return url
		}
		if _, err := n.QueryProjects().Create(ctx, tx, n.ProjectDraft{}.SetTeamID(f.teams[0].ID).SetSlug("disabled").SetEnabled(false)); err != nil {
			return err
		}
		missing, err := model.NewID[n.Team]()
		if err != nil {
			return err
		}
		for _, tc := range []struct {
			path                   string
			status, calls, handled int
		}{
			{location(f.teams[0].ID, "shared", "same"), 200, 3, 1},
			{location(f.teams[1].ID, "shared", "same"), 403, 3, 0},
			{location(missing, "shared", "same"), 404, 1, 0},
			{location(f.teams[0].ID, "absent", "same"), 404, 2, 0},
			{location(f.teams[0].ID, "disabled", "same"), 404, 2, 0},
			{location(f.teams[0].ID, "shared", "absent"), 404, 3, 0},
			{"/teams/not-a-uuid/projects/shared/tasks/same", 400, 0, 0},
		} {
			before, invoked := counter.Queries.Load(), handled
			route(t, router, ctx, tc.path, "", tc.status)
			if counter.Queries.Load()-before != int64(tc.calls) || handled-invoked != tc.handled {
				t.Error("resolver or domain call count changed")
			}
		}
		// Same slug under another team succeeds independently; no global slug fallback.
		for i, team := range f.teams {
			resources, err := n.Resolve(counter).Resolve(ctx, n.Path{Team: team.ID, Project: "shared", Task: "same"})
			if err != nil || resources.Parent.Child.ID != f.projects[i].ID || resources.Child.ID != f.tasks[i].ID {
				return errors.New("nested key escaped its parent")
			}
		}
		onlyBeta, err := n.QueryProjects().Create(ctx, tx, n.ProjectDraft{}.SetTeamID(f.teams[1].ID).SetSlug("beta-only").SetEnabled(true))
		if err != nil {
			return err
		}
		_, err = n.Projects(counter).Resolve(ctx, n.Path{Team: f.teams[0].ID, Project: onlyBeta.Slug})
		if !errors.Is(err, foundryhttp.NotFound) {
			return errors.New("global child fallback")
		}
		// Global alternate slug is ambiguous, scoped alternate slug is unique.
		if _, err := n.ProjectBySlug(counter).Resolve(ctx, n.Path{Project: "shared"}); err == nil || errors.Is(err, foundryhttp.NotFound) {
			return errors.New("duplicate global slug selected a row")
		}
		trace := &n.Trace{}
		if _, err := n.Resolve(counter).Resolve(n.WithTrace(ctx, trace), n.Path{Team: f.teams[0].ID, Project: "shared", Task: "same"}); err != nil || trace.Projects != 1 {
			return errors.New("project retrieval hook lost or duplicated")
		}
		veto := &n.Trace{Fail: true}
		route(t, router, n.WithTrace(ctx, veto), location(f.teams[0].ID, "shared", "same"), "", 500)
		if veto.Projects != 1 {
			return errors.New("retrieval error changed scope behavior")
		}
		eager := query.RelatedUnique(n.TeamRelations().Projects.With(n.ProjectRelations().Team), n.ProjectFields().Slug)
		before := counter.Queries.Load()
		loaded, err := eager.Find(ctx, counter, f.teams[0], "shared")
		if err != nil {
			return err
		}
		project, ok := loaded.Get()
		parent, set := project.Team.Get()
		team, present := parent.Get()
		if !ok || !set || !present || team.ID != f.teams[0].ID || counter.Queries.Load()-before != 2 {
			return errors.New("declared eager load or its accounting lost")
		}
		if _, err := n.QueryProjects().Delete(ctx, tx, f.projects[0].ID); err != nil {
			return err
		}
		route(t, router, ctx, location(f.teams[0].ID, "shared", "same"), "", 404)
		visible := query.RelatedUnique(n.TeamRelations().Projects.WithTrashed(), n.ProjectFields().Slug)
		if got, err := visible.Find(ctx, tx, f.teams[0], "shared"); err != nil || !got.IsSet() {
			return errors.New("explicit soft-delete visibility lost")
		}
		// A second duplicate inside the same parent fails closed, with no domain call.
		if _, err := n.QueryProjects().Create(ctx, tx, n.ProjectDraft{}.SetTeamID(f.teams[1].ID).SetSlug("shared").SetEnabled(true)); err != nil {
			return err
		}
		route(t, router, ctx, location(f.teams[1].ID, "shared", "same"), "", 500)
		return nil
	})
}

func TestPostgresTypedRequestAndResourcePolicyStages(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		ctx := t.Context()
		f, err := seed(ctx, tx)
		if err != nil {
			return err
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		provider := auth.DefineProvider("nested_teams", f.teams[0].FoundryReference(), func(_ context.Context, id model.ID[n.Team]) (value.Optional[n.Team], error) {
			if id == f.teams[0].ID {
				return value.Set(f.teams[0]), nil
			}
			return value.Optional[n.Team]{}, nil
		}, func(context.Context, n.Team) (bool, error) { return true, nil })
		proof, err := auth.NewProof(f.teams[0].FoundryReference(), auth.Authenticated)
		if err != nil {
			return err
		}
		strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[n.Team, model.ID[n.Team]]], error) {
			if token.Reveal() == "fixture" {
				return value.Set(proof), nil
			}
			return value.Optional[auth.Proof[n.Team, model.ID[n.Team]]]{}, nil
		})
		guard := auth.DefineGuard("nested_team", provider, strategy)
		registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
		if err != nil {
			return err
		}
		transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
		if err != nil {
			return err
		}
		base := n.Transport(foundryhttp.Guarded)
		secured := foundryhttp.RequireAuthentication(base, transport, guard).WithAuthorization(func(_ context.Context, _ n.Team, in n.TransportRequest) error {
			if in.Path.Project == "request-denied" {
				return foundryhttp.Forbidden
			}
			return nil
		})
		bound := modelbinding.BindAuthenticated(secured, n.Resolve(counter)).WithAuthorization(func(_ context.Context, actor n.Team, in n.Request) error {
			if actor.ID != in.Model.Parent.Parent.ID {
				return foundryhttp.Forbidden
			}
			return nil
		})
		handled := 0
		router, err := foundryhttp.NewRouter(bound.Handle(func(ctx context.Context, _ n.Team, in n.Request) (n.Reply, error) {
			handled++
			return n.Handle(ctx, in)
		}))
		if err != nil {
			return err
		}
		for _, tc := range []struct {
			team                   int
			slug, token            string
			status, calls, handled int
		}{{0, "shared", "fixture", 200, 3, 1}, {1, "shared", "fixture", 403, 3, 0}, {0, "request-denied", "fixture", 403, 0, 0}, {0, "shared", "invalid", 401, 0, 0}} {
			url, err := base.URL(ctx, n.Path{Team: f.teams[tc.team].ID, Project: n.Slug(tc.slug), Task: "same"}, foundryhttp.NoQuery{})
			if err != nil {
				return err
			}
			before, count := counter.Queries.Load(), handled
			route(t, router, ctx, url, tc.token, tc.status)
			if counter.Queries.Load()-before != int64(tc.calls) || handled-count != tc.handled {
				return errors.New("request/resource authorization crossed lookup boundary")
			}
		}
		before, err := base.Description()
		if err != nil {
			return err
		}
		after, err := bound.Description()
		if err != nil {
			return err
		}
		if before.Response.Schema.Root != after.Response.Schema.Root || strings.Contains(string(after.Response.Schema.Root), "Team") {
			return errors.New("persistence models leaked into transport DTO")
		}
		return nil
	})
}
