// Package queryfixture supplies isolated data shared by advanced query fixtures.
package queryfixture

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Run provides three users and three orders in a unique retained schema.
func Run(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	run(t, 3, func(i int, users []models.User, draft models.UserDraft) models.UserDraft {
		if i == 1 {
			draft = draft.SetIntroducerID(users[0].ID).SetNickname("Bee")
		}
		return draft
	}, check)
}

// RunJoins provides four users forming a referral graph and three orders.
// It shares schema creation and base seeding with Run, in an isolated schema.
func RunJoins(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	run(t, 4, func(i int, users []models.User, draft models.UserDraft) models.UserDraft {
		if i == 1 {
			draft = draft.SetStatus(models.StatusDisabled)
		}
		if i == 1 || i == 2 {
			draft = draft.SetIntroducerID(users[0].ID)
		}
		if i == 3 {
			draft = draft.SetIntroducerID(users[1].ID)
		}
		return draft
	}, check)
}

func run(t *testing.T, userCount int, customize func(int, []models.User, models.UserDraft) models.UserDraft, check func(*database.Tx, []models.User) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY,email_address text NOT NULL,age bigint NOT NULL,nickname text,status text NOT NULL,level smallint NOT NULL,birthday date,introducer_id uuid)`,
			`CREATE TABLE orders (id uuid PRIMARY KEY,buyer_id uuid NOT NULL,total_cents bigint NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		var users []models.User
		for i, email := range []string{"a@example.test", "b@example.test", "c@example.test", "d@example.test"}[:userCount] {
			draft := models.UserDraft{}.SetEmail(email).SetAge(20 + i).SetStatus(models.StatusActive).SetLevel(models.LevelBasic)
			draft = customize(i, users, draft)
			u, err := models.QueryUsers().Create(t.Context(), tx, draft)
			if err != nil {
				return err
			}
			users = append(users, u)
		}
		for i, buyer := range []model.ID[models.User]{users[0].ID, users[0].ID, users[1].ID} {
			if _, err := models.QueryOrders().Create(t.Context(), tx, models.OrderDraft{}.SetBuyerID(buyer).SetTotalCents(int64(i+1))); err != nil {
				return err
			}
		}
		return check(tx, users)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// NoQueries fails the test if a rejected/canceled operation reaches its executor.
func NoQueries(t *testing.T) database.Executor { return unexpectedExecutor{t: t} }

// CreateFriendshipTable adds the self-link schema for relationship query tests.
func CreateFriendshipTable(ctx context.Context, tx *database.Tx) error {
	_, err := tx.Exec(ctx, `CREATE TABLE friendships (id uuid PRIMARY KEY,from_id uuid NOT NULL,to_id uuid NOT NULL,note text NOT NULL)`)
	return err
}

type unexpectedExecutor struct {
	database.Executor
	t *testing.T
}

func (e unexpectedExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	e.t.Error("rejected query reached the executor")
	return nil, errors.New("unexpected query")
}
