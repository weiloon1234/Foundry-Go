package pivothooks_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/pivothooks"
	"github.com/weiloon1234/Foundry-Go/database"
)

// claimsUniqueViolation is an application error whose custom Is claims to be
// a unique violation without coming from the model's INSERT statement.
type claimsUniqueViolation struct{}

func (claimsUniqueViolation) Error() string        { return "application failure" }
func (claimsUniqueViolation) Is(target error) bool { return target == database.UniqueViolation }

// CreateOrFirst resolves only a unique violation of the model's own INSERT on
// its own table. Hook failures, hook writes and trigger writes that raise (or
// claim) a unique violation are returned rather than answered with the first
// matching row.
func TestCreateOrFirstResolvesOnlyTheModelInsertConflict(t *testing.T) {
	f := start(t)
	ctx := t.Context()
	for _, ddl := range []string{
		`CREATE UNIQUE INDEX hook_assignments_member ON hook_assignments(project_id, person_id)`,
		`CREATE UNIQUE INDEX hook_people_name ON hook_people(name)`,
		`CREATE FUNCTION hook_duplicate_person() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.role = 'duplicate' THEN
				INSERT INTO hook_people(id, name) VALUES (gen_random_uuid(), 'ann');
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE TRIGGER hook_duplicate_person BEFORE INSERT ON hook_assignments FOR EACH ROW EXECUTE FUNCTION hook_duplicate_person()`,
	} {
		if _, err := f.db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	a := pivothooks.AssignmentFields()
	lead, err := pivothooks.QueryHookAssignments().Create(ctx, f.db, pivothooks.AssignmentDraft{}.
		SetProjectID(f.project.ID).SetPersonID(f.people["ann"].ID).SetRole("lead"))
	if err != nil {
		t.Fatal(err)
	}
	// Every lookup below matches ann's existing assignment.
	project := pivothooks.QueryHookAssignments().Where(a.ProjectID.Eq(f.project.ID))
	draft := func(person, role string) pivothooks.AssignmentDraft {
		return pivothooks.AssignmentDraft{}.SetProjectID(f.project.ID).SetPersonID(f.people[person].ID).SetRole(role)
	}
	// The model's own INSERT conflicting on its own unique index resolves.
	found, err := project.Where(a.PersonID.Eq(f.people["ann"].ID)).CreateOrFirst(ctx, f.db, draft("ann", "member"))
	if err != nil || found.ID != lead.ID || found.Role != "lead" {
		t.Fatal("own unique conflict did not resolve to the stored model", found, err)
	}
	for name, test := range map[string]struct {
		role      string
		onCreated func(context.Context, *database.Tx) error
		want      func(error) bool
	}{
		"hook write violation": {role: "member", onCreated: func(ctx context.Context, tx *database.Tx) error {
			_, err := pivothooks.QueryHookPeople().Create(ctx, tx, pivothooks.PersonDraft{}.SetName("ann"))
			return err
		}, want: func(err error) bool { return errors.Is(err, database.UniqueViolation) }},
		"hook claims violation": {role: "member", onCreated: func(context.Context, *database.Tx) error {
			return claimsUniqueViolation{}
		}, want: func(err error) bool { return errors.As(err, new(claimsUniqueViolation)) }},
		"trigger violation on another table": {role: "duplicate", want: func(err error) bool {
			var failure *database.Error
			return errors.As(err, &failure) && failure.Code() == database.UniqueViolation && failure.Constraint() == "hook_people_name"
		}},
	} {
		t.Run(name, func(t *testing.T) {
			trace := &pivothooks.Trace{OnCreated: test.onCreated}
			created, err := project.CreateOrFirst(pivothooks.WithTrace(ctx, trace), f.db, draft("bob", test.role))
			if !test.want(err) {
				t.Fatal("foreign failure was not returned", created, err)
			}
			if created.ID == lead.ID {
				t.Fatal("foreign failure resolved to an unrelated stored model")
			}
			if links := f.links(t); len(links) != 1 || links["ann"] != "lead" {
				t.Fatal("failed creation left writes", links)
			}
		})
	}
}
