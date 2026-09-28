package softqueries_test

import (
	"testing"
	"time"

	"foundry.test/consumer/softqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func runSoft(t *testing.T, work func(*database.Tx, *testkit.Clock) error) {
	t.Helper()
	source := testkit.NewClock(time.Date(2031, 2, 3, 4, 5, 6, 123456789, time.UTC))
	pool := foundation.NewKey[*database.DB]("soft.pool")
	observers := foundation.Module{Name: "observers", OnRegister: func(r *foundation.Registrar) error {
		return softqueries.RegisterMemberObserver(r, pool, softqueries.NewMemberObserver("provider.member"), func(foundation.Resolver) (func() softqueries.MemberHooks, error) {
			return func() softqueries.MemberHooks { return softqueries.Hooks("provider") }, nil
		})
	}}
	app := testkit.Start(t, foundry.New(foundation.WithClock(source)).Register(postgres.Module("database", pool, pgtest.Config(t))).Register(observers))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, ddl := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE soft_members(id uuid PRIMARY KEY,name text NOT NULL,parent_id uuid REFERENCES soft_members(id),created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,removed_on timestamptz)`,
			`CREATE UNIQUE INDEX soft_member_name_active ON soft_members(name) WHERE removed_on IS NULL`,
			`CREATE TABLE soft_groups(code text PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
			`CREATE TABLE soft_memberships(id uuid PRIMARY KEY,member_id uuid NOT NULL REFERENCES soft_members(id),group_code text NOT NULL REFERENCES soft_groups(code),deleted_at timestamptz)`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		return work(tx, source)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type wrappedTransactor struct{ database.Transactor }
