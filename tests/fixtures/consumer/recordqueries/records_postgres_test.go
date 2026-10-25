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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type userAlias struct{}
type orderAlias struct{}
type reportAlias struct{}

func TestPostgresCompleteRecordsFromJoins(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		people := rowset.As[userAlias](models.Users.Query(), "person")
		orders := rowset.As[orderAlias](models.Orders.Query(), "purchase")
		p, o := rowset.Scope(people), rowset.Scope(orders)
		joined := rowset.InnerJoin(people, orders, rowset.On(expr.At(p, models.Users.ID), expr.At(o, models.Orders.BuyerID)))
		scope := rowset.LeftScope(joined, rowset.Scope(people))
		fields := scope
		builder := rowset.SelectRecord(joined, scope).OrderBy(expr.At(fields, models.Users.Email).Asc())
		rows, err := builder.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(rows, []models.User{users[0], users[0], users[1]}) {
			t.Fatal("whole model selection changed decoding or duplicate join rows")
		}
		one, err := rowset.ReadRecord(builder.Where(expr.At(fields, models.Users.ID).Eq(users[1].ID))).RequireFirst(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(one, users[1]) || one.Orders.IsLoaded() {
			t.Fatal("whole model selection lost concrete model or loaded relations implicitly", err)
		}
		if row, err := rowset.ReadRecord(builder.Where(expr.At(fields, models.Users.ID).Eq(users[2].ID))).First(t.Context(), tx); err != nil || row.IsSet() {
			t.Error("missing joined model was not optional", err)
		}
		if _, err := rowset.ReadRecord(builder.Limit(0)).RequireFirst(t.Context(), tx); !errors.Is(err, database.NotFound) {
			t.Error("required model ignored empty result", err)
		}
		if n, err := rowset.ReadRecord(builder.Offset(1).Limit(1)).Count(t.Context(), tx); err != nil || n != 1 {
			t.Error("selected record count ignored window", err)
		}
		if found, err := rowset.ReadRecord(builder.Limit(0)).Exists(t.Context(), tx); err != nil || found {
			t.Error("selected record exists ignored zero limit", err)
		}

		left := rowset.LeftJoin(people, orders, rowset.On(expr.At(p, models.Users.ID), expr.At(o, models.Orders.BuyerID)))
		leftScope := rowset.LeftScope(left, rowset.Scope(people))
		leftFields := leftScope
		leftRows, err := rowset.SelectRecord(left, leftScope).OrderBy(expr.At(leftFields, models.Users.Email).Asc()).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(leftRows, []models.User{users[0], users[0], users[1], users[2]}) {
			t.Fatal("preserved left model lost unmatched row", err)
		}
		right := rowset.RightJoin(orders, people, rowset.On(expr.At(o, models.Orders.BuyerID), expr.At(p, models.Users.ID)))
		rightScope := rowset.RightScope(right, rowset.Scope(people))
		if row, err := rowset.ReadRecord(rowset.SelectRecord(right, rightScope).Where(expr.At(rightScope, models.Users.ID).Eq(users[2].ID))).RequireFirst(t.Context(), tx); err != nil || !reflect.DeepEqual(row, users[2]) {
			t.Error("preserved right model lost unmatched row", err)
		}
		// A complete joined result and a plain model query share the same record contract.
		combined := rowset.Union(builder, rowset.ModelRecord(models.Users.Query()))
		cf := combined.Scope()
		if rows, err := combined.OrderBy(expr.At(cf, models.Users.Email).Asc()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(rows, users) {
			t.Error("joined model could not compose with typed set", err)
		}
		common := rowset.CTE("joined_people", builder)
		alias := rowset.AsRecord[userAlias](common, "selected")
		if n, err := rowset.ReadRecord(rowset.SelectRecord(alias, rowset.Scope(alias))).Count(t.Context(), tx); err != nil || n != 3 {
			t.Error("selected record lost CTE dependency or decoder", err)
		}

		stop := errors.New("stop selected record stream")
		if err := rowset.EachRead(t.Context(), tx, builder, func(models.User) error { return stop }); !errors.Is(err, stop) {
			t.Error("record stream lost callback failure", err)
		}
		if n, err := rowset.ReadRecord(builder).Count(t.Context(), tx); err != nil || n != 3 {
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
	u := models.Users
	report := rowset.Select(rowset.From(models.Users.Query()), reports.UserSummaries.Row,
		rowset.Map(reports.UserSummaries.ID, expr.Value(u.ID)),
		rowset.Map(reports.UserSummaries.Email, expr.Value(u.Email)),
		rowset.Map(reports.UserSummaries.Nickname, expr.Value(u.Nickname)),
		rowset.Map(reports.UserSummaries.Status, expr.Value(u.Status)),
	)
	summaries := rowset.AsRecord[reportAlias](rowset.RecordOf(report), "summary")
	orders := rowset.As[orderAlias](models.Orders.Query(), "purchase")
	sf := rowset.Scope(summaries)
	o := rowset.Scope(orders)
	joined := rowset.InnerJoin(summaries, orders, rowset.OnEqual(rowset.At(sf, reports.UserSummaries.ID), expr.At(o, models.Orders.BuyerID)))
	scope := rowset.LeftScope(joined, rowset.Scope(summaries))
	fields := scope
	selected := rowset.SelectRecord(joined, scope).Where(rowset.At(fields, reports.UserSummaries.ID).Eq(users[1].ID))
	row, err := rowset.ReadRecord(selected).RequireFirst(t.Context(), tx)
	if err != nil {
		return err
	}
	want := reports.UserSummary{ID: users[1].ID, Email: users[1].Email, Nickname: users[1].Nickname, Status: users[1].Status}
	if !reflect.DeepEqual(row, want) {
		t.Error("whole projection lost field aliases, nullability or codecs")
	}
	combined := rowset.Union(selected, rowset.RecordOf(report))
	if n, err := rowset.ReadRecord(rowset.SelectRecord(combined, combined.Scope())).Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("complete projection/set selection did not compose", err)
	}
	return nil
}

func TestInvalidRecordSelectionNeverExecutes(t *testing.T) {
	q := models.Users.Query()
	var nilSource *rowset.Input[models.User]
	for _, selected := range []rowset.RecordRead[models.User, models.User]{
		rowset.SelectRecord(rowset.From(q), expr.Scope[models.User, models.User]{}),
		rowset.SelectRecord(rowset.From(q), rowset.ModelScope(orm.Query[models.User]{})),
		rowset.SelectRecord(nilSource, rowset.ModelScope(q)),
		rowset.SelectRecord(rowset.From(q.With(models.UserLinks.Orders)), rowset.ModelScope(q)),
		rowset.SelectRecord(rowset.From(q), rowset.ModelScope(q)).Where(expr.ModelPredicate(models.Users.Status.Eq(models.Status("invalid")))),
	} {
		if rows, err := selected.All(t.Context(), queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, fault.Invalid) {
			t.Error("invalid record selection reached executor", err)
		}
	}
}
