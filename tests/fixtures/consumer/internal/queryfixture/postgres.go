// Package queryfixture supplies isolated data shared by advanced query fixtures.
package queryfixture

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Run provides three users and three orders in a unique retained schema.
func Run(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	runDefault(t, false, check)
}

// RunStable uses fixed seed identities for repeatable capture/replay in fresh schemas.
func RunStable(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	runDefault(t, true, check)
}

func runDefault(t *testing.T, stable bool, check func(*database.Tx, []models.User) error) {
	t.Helper()
	run(t, 3, stable, func(i int, users []models.User, assignments []orm.Assignment[models.User]) []orm.Assignment[models.User] {
		assignments = append(assignments, models.Users.Status.Set(models.StatusActive))
		if i == 1 {
			assignments = append(assignments, models.Users.IntroducerID.Set(users[0].ID), models.Users.Nickname.Set("Bee"))
		}
		return assignments
	}, check)
}

// RunJoins provides four users forming a referral graph and three orders.
// It shares schema creation and base seeding with Run, in an isolated schema.
func RunJoins(t *testing.T, check func(*database.Tx, []models.User) error) {
	t.Helper()
	run(t, 4, false, func(i int, users []models.User, assignments []orm.Assignment[models.User]) []orm.Assignment[models.User] {
		if i == 1 {
			assignments = append(assignments, models.Users.Status.Set(models.StatusDisabled))
		} else {
			assignments = append(assignments, models.Users.Status.Set(models.StatusActive))
		}
		if i == 1 || i == 2 {
			assignments = append(assignments, models.Users.IntroducerID.Set(users[0].ID))
		}
		if i == 3 {
			assignments = append(assignments, models.Users.IntroducerID.Set(users[1].ID))
		}
		return assignments
	}, check)
}

func run(t *testing.T, userCount int, stable bool, customize func(int, []models.User, []orm.Assignment[models.User]) []orm.Assignment[models.User], check func(*database.Tx, []models.User) error) {
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
			assignments := []orm.Assignment[models.User]{(models.User{Email: email}).CurrentEmailAssignment(), models.Users.Age.Set(20 + i), models.Users.Level.Set(models.LevelBasic)}
			if stable {
				id, err := model.ParseID[models.User](fmt.Sprintf("10000000-0000-0000-0000-%012d", i+1))
				if err != nil {
					return err
				}
				assignments = append(assignments, models.Users.Key().Set(id))
			}
			assignments = customize(i, users, assignments)
			u, err := models.Users.Insert(t.Context(), tx, assignments...)
			if err != nil {
				return err
			}
			users = append(users, u)
		}
		for i, buyer := range []model.ID[models.User]{users[0].ID, users[0].ID, users[1].ID} {
			assignments := []orm.Assignment[models.Order]{models.Orders.BuyerID.Set(buyer), models.Orders.TotalCents.Set(int64(i + 1))}
			if stable {
				id, err := model.ParseID[models.Order](fmt.Sprintf("20000000-0000-0000-0000-%012d", i+1))
				if err != nil {
					return err
				}
				assignments = append(assignments, models.Orders.Key().Set(id))
			}
			if _, err := models.Orders.Insert(t.Context(), tx, assignments...); err != nil {
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
