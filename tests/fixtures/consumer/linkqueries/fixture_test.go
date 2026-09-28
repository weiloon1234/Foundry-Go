package linkqueries_test

import (
	"context"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func linkDatabase(t *testing.T) (*database.DB, *testkit.Clock, string) {
	t.Helper()
	source := testkit.NewClock(time.Date(2032, 3, 4, 5, 6, 7, 123456789, time.UTC))
	pool := foundation.NewKey[*database.DB]("link.pool")
	observers := foundation.Module{Name: "observers", OnRegister: func(r *foundation.Registrar) error {
		return linkqueries.RegisterMembershipObserver(r, pool, linkqueries.NewMembershipObserver("provider.link"), func(foundation.Resolver) (func() linkqueries.MembershipHooks, error) {
			return func() linkqueries.MembershipHooks { return linkqueries.Hooks("provider") }, nil
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
			`CREATE TABLE link_members(id uuid PRIMARY KEY,name text NOT NULL,alias text,deleted_at timestamptz)`,
			`CREATE TABLE link_groups(code text PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
			`CREATE TABLE link_memberships(id uuid PRIMARY KEY,member_id uuid NOT NULL REFERENCES link_members(id),group_code text NOT NULL REFERENCES link_groups(code),role text NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,deleted_at timestamptz)`,
			`CREATE TABLE link_natural(id uuid PRIMARY KEY,member_name text NOT NULL,group_name text NOT NULL)`,
			`CREATE TABLE link_nullable(id uuid PRIMARY KEY,member_alias text,group_name text)`,
			`CREATE TABLE link_friendships(id uuid PRIMARY KEY,from_id uuid NOT NULL REFERENCES link_members(id),to_id uuid NOT NULL REFERENCES link_members(id))`,
			`CREATE TABLE link_archives(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),member_id uuid NOT NULL REFERENCES link_members(id),name text NOT NULL,alias text,tag text NOT NULL,counter bigint NOT NULL DEFAULT 42,amount numeric NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, source, namespace
}

func linkTransaction(ctx context.Context, db *database.DB, namespace string, work func(*database.Tx) error) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		return work(tx)
	})
}

func runLinks(t *testing.T, work func(*database.Tx, *testkit.Clock) error) {
	t.Helper()
	db, source, namespace := linkDatabase(t)
	if err := linkTransaction(t.Context(), db, namespace, func(tx *database.Tx) error { return work(tx, source) }); err != nil {
		t.Fatal(err)
	}
}

func endpoints(t *testing.T, tx *database.Tx) (linkqueries.Member, linkqueries.Group, error) {
	t.Helper()
	member, err := linkqueries.QueryLinkMembers().Create(t.Context(), tx, linkqueries.MemberDraft{}.SetName("member"))
	if err != nil {
		return member, linkqueries.Group{}, err
	}
	group, err := linkqueries.QueryLinkGroups().Create(t.Context(), tx, linkqueries.GroupDraft{}.SetCode("staff").SetName("staff"))
	return member, group, err
}

type wrappedTransactor struct{ database.Transactor }
