package recordqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type userAlias struct{}
type orderAlias struct{}
type reportAlias struct{}

func TestPostgresCompleteRecordsFromJoins(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		people := query.As[userAlias](models.QueryUsers(), "person")
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		p, o := models.UserFieldsAt(people.Scope()), models.OrderFieldsAt(orders.Scope())
		joined := query.InnerJoin(people, orders, query.On(p.ID, o.BuyerID))
		scope := query.LeftScope(joined, people.Scope())
		fields := models.UserFieldsAt(scope)
		builder := query.SelectRecord(joined, scope).OrderBy(fields.Email.Asc())
		rows, err := builder.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rows, []models.User{users[0], users[0], users[1]}) {
			t.Fatal("whole model selection changed decoding or duplicate join rows")
		}
		one, err := builder.Where(fields.ID.Eq(users[1].ID)).RequireFirst(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(one, users[1]) || one.Orders.IsLoaded() {
			t.Fatal("whole model selection lost concrete model or loaded relations implicitly", err)
		}
		if row, err := builder.Where(fields.ID.Eq(users[2].ID)).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Error("missing joined model was not optional", err)
		}
		if _, err := builder.Limit(0).RequireFirst(t.Context(), tx); !errors.Is(err, database.NotFound) {
			t.Error("required model ignored empty result", err)
		}
		if n, err := builder.Offset(1).Limit(1).Count(t.Context(), tx); err != nil || n != 1 {
			t.Error("selected record count ignored window", err)
		}
		if found, err := builder.Limit(0).Exists(t.Context(), tx); err != nil || found {
			t.Error("selected record exists ignored zero limit", err)
		}

		left := query.LeftJoin(people, orders, query.On(p.ID, o.BuyerID))
		leftScope := query.LeftScope(left, people.Scope())
		leftFields := models.UserFieldsAt(leftScope)
		leftRows, err := query.SelectRecord(left, leftScope).OrderBy(leftFields.Email.Asc()).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(leftRows, []models.User{users[0], users[0], users[1], users[2]}) {
			t.Fatal("preserved left model lost unmatched row", err)
		}
		right := query.RightJoin(orders, people, query.On(o.BuyerID, p.ID))
		rightScope := query.RightScope(right, people.Scope())
		if row, err := query.SelectRecord(right, rightScope).Where(models.UserFieldsAt(rightScope).ID.Eq(users[2].ID)).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[2]) {
			t.Error("preserved right model lost unmatched row", err)
		}
		// A complete joined result and a plain model query share the same record contract.
		combined := builder.Union(models.QueryUsers())
		cf := models.UserFieldsAt(combined.Scope())
		if rows, err := combined.OrderBy(cf.Email.Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, users) {
			t.Error("joined model could not compose with typed set", err)
		}
		common := query.CTE("joined_people", builder)
		alias := query.As[userAlias](common, "selected")
		if n, err := query.SelectRecord(alias, alias.Scope()).Count(t.Context(), tx); err != nil || n != 3 {
			t.Error("selected record lost CTE dependency or decoder", err)
		}

		stop := errors.New("stop selected record stream")
		if err := builder.Each(t.Context(), tx, func(models.User) error { return stop }); !errors.Is(err, stop) {
			t.Error("record stream lost callback failure", err)
		}
		if n, err := builder.Count(t.Context(), tx); err != nil || n != 3 {
			t.Error("record stream failed to release rows", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if rows, err := builder.All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
			t.Error("canceled record selection reached executor", err)
		}
		if err := checkProjectionRecord(t, tx, users); err != nil {
			return err
		}
		// Corrupt only this isolated fixture, after successful rows, to prove all-or-error hydration.
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, users[1].ID.String()); err != nil {
			return err
		}
		if rows, err := builder.All(t.Context(), tx); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("record selection returned partial models after codec failure", err)
		}
		return nil
	})
}

func checkProjectionRecord(t *testing.T, tx *database.Tx, users []models.User) error {
	u := models.UserFields()
	report := reports.SelectUserSummary(models.QueryUsers(), reports.UserSummarySelection[models.User]{ID: u.ID.Value(), Email: u.Email.Value(), Nickname: u.Nickname.Value(), Status: u.Status.Value()})
	summaries := query.As[reportAlias](report, "summary")
	orders := query.As[orderAlias](models.QueryOrders(), "purchase")
	sf := reports.UserSummaryFieldsAt(summaries.Scope())
	o := models.OrderFieldsAt(orders.Scope())
	joined := query.InnerJoin(summaries, orders, query.On(sf.ID, o.BuyerID))
	scope := query.LeftScope(joined, summaries.Scope())
	fields := reports.UserSummaryFieldsAt(scope)
	selected := query.SelectRecord(joined, scope).Where(fields.ID.Eq(users[1].ID))
	row, err := selected.RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	want := reports.UserSummary{ID: users[1].ID, Email: users[1].Email, Nickname: users[1].Nickname, Status: users[1].Status}
	if !reflect.DeepEqual(row, want) {
		t.Error("whole projection lost field aliases, nullability or codecs")
	}
	combined := selected.Union(report)
	if n, err := query.SelectRecord(combined, combined.Scope()).Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("complete projection/set selection did not compose", err)
	}
	return nil
}

func TestInvalidRecordSelectionNeverExecutes(t *testing.T) {
	q := models.QueryUsers()
	var nilSource *models.UserQuery
	for _, selected := range []query.ProjectionQuery[models.User, models.User]{
		query.SelectRecord(q, query.RecordScope[models.User, models.User]{}),
		query.SelectRecord(q, query.DeclareModelScope[models.User]("users")),
		query.SelectRecord(nilSource, q.Scope()),
		query.SelectRecord(q.With(models.UserRelations().Orders), q.Scope()),
		query.SelectRecord(q, q.Scope()).Where(models.UserFields().Status.Eq(models.Status("invalid"))),
	} {
		if rows, err := selected.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid record selection reached executor", err)
		}
	}
}
