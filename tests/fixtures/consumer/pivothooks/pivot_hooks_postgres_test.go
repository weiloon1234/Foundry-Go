package pivothooks_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"foundry.test/consumer/pivothooks"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type fixture struct {
	db      *database.DB
	project pivothooks.Project
	people  map[string]pivothooks.Person
	names   map[model.ID[pivothooks.Person]]string
}

// start assembles an application whose pivot has local hooks and a registered
// provider observer, so every normal pivot write runs both.
func start(t *testing.T) fixture {
	t.Helper()
	scope := pgtest.Isolate(t)
	pool := foundation.NewKey[*database.DB]("pivot.pool")
	observers := foundation.Module{Name: "observers", OnRegister: func(r *foundation.Registrar) error {
		return pivothooks.RegisterAssignmentObserver(r, pool, pivothooks.NewAssignmentObserver("provider.assignment"), func(foundation.Resolver) (func() pivothooks.AssignmentHooks, error) {
			return func() pivothooks.AssignmentHooks { return pivothooks.Hooks("provider") }, nil
		})
	}}
	app := testkit.Start(t, foundry.New().Register(postgres.Module("database", pool, scope.Config())).Register(observers))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE hook_projects(id uuid PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE hook_people(id uuid PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE hook_assignments(id uuid PRIMARY KEY, project_id uuid NOT NULL REFERENCES hook_projects(id), person_id uuid NOT NULL REFERENCES hook_people(id), role text NOT NULL)`,
	} {
		if _, err := db.Exec(t.Context(), ddl); err != nil {
			t.Fatal(err)
		}
	}
	f := fixture{db: db, people: make(map[string]pivothooks.Person), names: make(map[model.ID[pivothooks.Person]]string)}
	if f.project, err = pivothooks.QueryHookProjects().Create(t.Context(), db, pivothooks.ProjectDraft{}.SetName("apollo")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ann", "bob", "cid", "dee"} {
		person, err := pivothooks.QueryHookPeople().Create(t.Context(), db, pivothooks.PersonDraft{}.SetName(name))
		if err != nil {
			t.Fatal(err)
		}
		f.people[name], f.names[person.ID] = person, name
	}
	return f
}

func (f fixture) persons(names ...string) []pivothooks.Person {
	result := make([]pivothooks.Person, len(names))
	for i, name := range names {
		result[i] = f.people[name]
	}
	return result
}

// links reads the stored pivots as person name -> role.
func (f fixture) links(t *testing.T) map[string]string {
	t.Helper()
	stored, err := pivothooks.QueryHookAssignments().Where(pivothooks.AssignmentFields().ProjectID.Eq(f.project.ID)).All(t.Context(), f.db)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string, len(stored))
	for _, assignment := range stored {
		result[f.names[assignment.PersonID]] = assignment.Role
	}
	return result
}

// events counts each hook per pivot, keyed "owner.hook/person".
func (f fixture) events(trace *pivothooks.Trace) map[string]int {
	counts := make(map[string]int)
	for _, event := range trace.Events {
		pivot, present := event.After.Get()
		if before, ok := event.Before.Get(); ok {
			pivot, present = before, true
		}
		person := "?"
		if present {
			person = f.names[pivot.PersonID]
		}
		counts[event.Name+"/"+person]++
	}
	return counts
}

func names(f fixture, pivots []pivothooks.Assignment) []string {
	result := make([]string, len(pivots))
	for i, pivot := range pivots {
		result[i] = f.names[pivot.PersonID]
	}
	slices.Sort(result)
	return result
}

// expected builds exactly-once counts for both owners of each lifecycle.
func expected(creates, updates, deletes []string) map[string]int {
	counts := make(map[string]int)
	for _, owner := range []string{"local", "provider"} {
		for _, person := range creates {
			counts[owner+".creating/?"]++
			counts[owner+".created/"+person]++
		}
		for _, person := range updates {
			counts[owner+".updating/"+person]++
			counts[owner+".updated/"+person]++
		}
		for _, person := range deletes {
			counts[owner+".deleting/"+person]++
			counts[owner+".deleted/"+person]++
		}
	}
	return counts
}

func TestPivotLifecycleRunsOncePerChangedLink(t *testing.T) {
	f := start(t)
	members := pivothooks.ProjectRelations().Members
	member := pivothooks.AssignmentDraft{}.SetRole("member")
	run := func(t *testing.T, operation func(context.Context) error) *pivothooks.Trace {
		t.Helper()
		trace := &pivothooks.Trace{}
		if err := operation(pivothooks.WithTrace(t.Context(), trace)); err != nil {
			t.Fatal(err)
		}
		return trace
	}
	check := func(t *testing.T, trace *pivothooks.Trace, creates, updates, deletes []string, links map[string]string) {
		t.Helper()
		if got, want := f.events(trace), expected(creates, updates, deletes); !maps.Equal(got, want) {
			t.Fatalf("hook events\n got: %v\nwant: %v", got, want)
		}
		if len(trace.Committed) != len(creates)+len(updates)+len(deletes) {
			t.Fatal("after-commit callbacks", trace.Committed)
		}
		if got := f.links(t); !maps.Equal(got, links) {
			t.Fatalf("stored links %v, want %v", got, links)
		}
	}

	var result struct{ attached, detached, updated []pivothooks.Assignment }
	sync := func(ctx context.Context) error {
		c, err := members.Sync(ctx, f.db, f.project, f.persons("ann", "bob"), member, nil)
		result.attached, result.detached, result.updated = c.Attached, c.Detached, c.Updated
		return err
	}
	trace := run(t, sync)
	check(t, trace, []string{"ann", "bob"}, nil, nil, map[string]string{"ann": "member", "bob": "member"})
	if !slices.Equal(names(f, result.attached), []string{"ann", "bob"}) || len(result.detached)+len(result.updated) != 0 {
		t.Fatal("initial sync changes", result)
	}
	for _, event := range trace.Events {
		if event.Name == "local.created" && (event.Before.IsSet() || !event.After.IsSet()) {
			t.Fatal("created hook state", event)
		}
	}

	// Sync to bob and cid, updating retained links: ann is removed, bob updated,
	// cid created; each runs its own lifecycle exactly once.
	trace = run(t, func(ctx context.Context) error {
		c, err := members.Sync(ctx, f.db, f.project, f.persons("bob", "cid"), member, pivothooks.AssignmentDraft{}.SetRole("lead"))
		result.attached, result.detached, result.updated = c.Attached, c.Detached, c.Updated
		return err
	})
	check(t, trace, []string{"cid"}, []string{"bob"}, []string{"ann"}, map[string]string{"bob": "lead", "cid": "member"})
	if !slices.Equal(names(f, result.attached), []string{"cid"}) || !slices.Equal(names(f, result.detached), []string{"ann"}) ||
		!slices.Equal(names(f, result.updated), []string{"bob"}) || result.updated[0].Role != "lead" || result.detached[0].Role != "member" {
		t.Fatal("sync changes do not match the database", result)
	}
	for _, event := range trace.Events {
		switch event.Name {
		case "local.updated":
			before, _ := event.Before.Get()
			after, _ := event.After.Get()
			if before.Role != "member" || after.Role != "lead" {
				t.Fatal("updated hook state", event)
			}
		case "local.deleted":
			before, _ := event.Before.Get()
			if !event.Before.IsSet() || event.After.IsSet() || before.Role != "member" {
				t.Fatal("deleted hook state", event)
			}
		}
	}

	// Retained links without an update draft run no hooks.
	trace = run(t, func(ctx context.Context) error {
		c, err := members.SyncWithoutDetaching(ctx, f.db, f.project, f.persons("bob", "dee"), member, nil)
		result.attached, result.detached, result.updated = c.Attached, c.Detached, c.Updated
		return err
	})
	check(t, trace, []string{"dee"}, nil, nil, map[string]string{"bob": "lead", "cid": "member", "dee": "member"})
	if !slices.Equal(names(f, result.attached), []string{"dee"}) || len(result.detached)+len(result.updated) != 0 {
		t.Fatal("sync without detaching changes", result)
	}

	trace = run(t, func(ctx context.Context) error {
		c, err := members.Toggle(ctx, f.db, f.project, f.persons("cid", "ann"), member)
		result.attached, result.detached, result.updated = c.Attached, c.Detached, c.Updated
		return err
	})
	check(t, trace, []string{"ann"}, nil, []string{"cid"}, map[string]string{"ann": "member", "bob": "lead", "dee": "member"})
	if !slices.Equal(names(f, result.attached), []string{"ann"}) || !slices.Equal(names(f, result.detached), []string{"cid"}) {
		t.Fatal("toggle changes", result)
	}

	trace = run(t, func(ctx context.Context) error {
		updated, err := members.UpdateExistingPivot(ctx, f.db, f.project, f.people["dee"], pivothooks.AssignmentDraft{}.SetRole("owner"))
		result.updated = updated
		return err
	})
	check(t, trace, nil, []string{"dee"}, nil, map[string]string{"ann": "member", "bob": "lead", "dee": "owner"})
	if !slices.Equal(names(f, result.updated), []string{"dee"}) || result.updated[0].Role != "owner" {
		t.Fatal("update existing pivot changes", result.updated)
	}

	trace = run(t, func(ctx context.Context) error {
		attached, err := members.AttachMany(ctx, f.db, f.project, f.persons("cid"), member)
		result.attached = attached
		return err
	})
	check(t, trace, []string{"cid"}, nil, nil, map[string]string{"ann": "member", "bob": "lead", "cid": "member", "dee": "owner"})

	trace = run(t, func(ctx context.Context) error {
		detached, err := members.DetachMany(ctx, f.db, f.project, f.persons("ann", "bob"))
		result.detached = detached
		return err
	})
	check(t, trace, nil, nil, []string{"ann", "bob"}, map[string]string{"cid": "member", "dee": "owner"})
	if !slices.Equal(names(f, result.detached), []string{"ann", "bob"}) {
		t.Fatal("detach many changes", result.detached)
	}

	trace = run(t, func(ctx context.Context) error {
		detached, err := members.DetachAll(ctx, f.db, f.project)
		result.detached = detached
		return err
	})
	check(t, trace, nil, nil, []string{"cid", "dee"}, map[string]string{})
	if !slices.Equal(names(f, result.detached), []string{"cid", "dee"}) {
		t.Fatal("detach all changes", result.detached)
	}
}

func TestPivotHookFailureRollsBackTheWholeOperation(t *testing.T) {
	f := start(t)
	members := pivothooks.ProjectRelations().Members
	member := pivothooks.AssignmentDraft{}.SetRole("member")
	if _, err := members.Sync(t.Context(), f.db, f.project, f.persons("ann", "bob"), member, nil); err != nil {
		t.Fatal(err)
	}
	before := f.links(t)
	for _, veto := range []struct {
		hook  string
		count int
		run   func(context.Context) error
	}{
		// ann removed and bob updated before dee's creation fails.
		{"provider.created", 2, func(ctx context.Context) error {
			_, err := members.Sync(ctx, f.db, f.project, f.persons("bob", "cid", "dee"), member, pivothooks.AssignmentDraft{}.SetRole("lead"))
			return err
		}},
		{"local.updating", 1, func(ctx context.Context) error {
			_, err := members.UpdateExistingPivot(ctx, f.db, f.project, f.people["bob"], pivothooks.AssignmentDraft{}.SetRole("lead"))
			return err
		}},
		{"local.deleted", 2, func(ctx context.Context) error {
			_, err := members.DetachAll(ctx, f.db, f.project)
			return err
		}},
		{"provider.deleting", 1, func(ctx context.Context) error {
			_, err := members.Toggle(ctx, f.db, f.project, f.persons("ann", "cid"), member)
			return err
		}},
		{"local.creating", 2, func(ctx context.Context) error {
			_, err := members.AttachMany(ctx, f.db, f.project, f.persons("cid", "dee"), member)
			return err
		}},
	} {
		t.Run(fmt.Sprint(veto.hook, veto.count), func(t *testing.T) {
			trace := &pivothooks.Trace{VetoAt: veto.hook, VetoCount: veto.count}
			if err := veto.run(pivothooks.WithTrace(t.Context(), trace)); !errors.Is(err, pivothooks.Veto) {
				t.Fatal("hook failure was not returned", err)
			}
			if got := f.links(t); !maps.Equal(got, before) {
				t.Fatalf("a failed operation left partial links: %v, want %v", got, before)
			}
			if len(trace.Committed) != 0 {
				t.Fatal("after-commit callbacks ran for a rolled-back operation", trace.Committed)
			}
		})
	}
}
