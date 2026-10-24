package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresInsertOrFirstRejectsCrossSchemaIndexCollision(t *testing.T) {
	own, foreign := pgtest.Isolate(t), pgtest.Isolate(t)
	db := own.Open(t)
	ctx := t.Context()
	other := `"` + foreign.Schema() + `"."members"`
	for _, sql := range []string{
		`CREATE TABLE members(id bigint PRIMARY KEY,rank bigint NOT NULL,CONSTRAINT same_rank_key UNIQUE(rank))`,
		`INSERT INTO members VALUES (1,7)`,
		`CREATE TABLE ` + other + `(id bigint PRIMARY KEY,rank bigint NOT NULL,CONSTRAINT same_rank_key UNIQUE(rank))`,
		`INSERT INTO ` + other + ` VALUES (1,7)`,
		`CREATE FUNCTION collide_elsewhere() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO ` + other + ` VALUES (2,7); RETURN NEW; END $$`,
		`CREATE TRIGGER collide_before BEFORE INSERT ON members FOR EACH ROW EXECUTE FUNCTION collide_elsewhere()`,
	} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	rank := NewScalarField[memberRow, int64]("members", "rank", codec.Signed[int64]())
	got, err := memberQuery().Where(rank.Eq(7)).InsertOrFirst(ctx, db, memberDraft{id: 2, rank: 7})
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Schema() != foreign.Schema() || failure.Table() != "members" || failure.Constraint() != "same_rank_key" || got.ID != 0 {
		t.Fatal("foreign-schema index collision resolved as success", err)
	}
}
