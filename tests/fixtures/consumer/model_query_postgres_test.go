package consumer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedModelReads(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	firstID, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	entryID, err := model.NewID[models.LedgerEntry]()
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		statements := []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
			`CREATE TABLE countries (code text PRIMARY KEY, name text NOT NULL)`,
			`CREATE TABLE ledger_entries (id uuid PRIMARY KEY, amount numeric NOT NULL, balance numeric)`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO users (id,email_address,age,nickname,status,level,birthday,introducer_id) VALUES ($1,$2,21,NULL,'active',1,'2000-02-29',NULL),($3,$4,35,'second','disabled',2,NULL,$1)`, firstID.String(), "literal_%!'@example.test", secondID.String(), "literal-aa@example.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO countries (code,name) VALUES ('MY','Malaysia')`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO ledger_entries (id,amount,balance) VALUES ($1,$2,NULL)`, entryID.String(), "9007199254740993.123456789012345678901"); err != nil {
			return err
		}

		fields := models.UserFields()
		base := models.QueryUsers().OrderBy(fields.Age.Asc())
		users, err := base.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(users) != 2 || users[0].ID != firstID || users[0].Email != "literal_%!'@example.test" || users[0].Status != models.StatusActive || users[0].Level != models.LevelBasic || !users[0].Nickname.IsNull() || users[0].Scratch != nil || users[1].IntroducerID != value.Of(firstID) {
			t.Error("generated complete model hydration changed fields")
		}
		birthday, ok := users[0].Birthday.Get()
		if !ok || birthday.String() != "2000-02-29" {
			t.Error("nullable date hydration failed")
		}
		byBirthday := models.QueryUsers().OrderBy(fields.Birthday.Asc())
		cursorPage, err := byBirthday.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1})
		if err != nil {
			return err
		}
		nextCursor, ok := cursorPage.Next.Get()
		if !ok || len(cursorPage.Items) != 1 || cursorPage.Items[0].ID != firstID {
			return errors.New("nullable date cursor did not start at the non-null row")
		}
		lastPage, err := byBirthday.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1, After: value.Set(nextCursor)})
		if err != nil {
			return err
		}
		if len(lastPage.Items) != 1 || lastPage.Items[0].ID != secondID || lastPage.Next.IsSet() {
			t.Error("temporal/UUID cursor lost its exact boundary")
		}
		match, err := base.Where(fields.Email.Contains("_%!'")).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		if match.ID != firstID {
			t.Error("Contains treated literal wildcard characters as operators")
		}
		matches, err := base.Where(query.Or(fields.Age.Lt(22), fields.Status.Eq(models.StatusDisabled)), fields.ID.In(firstID, secondID)).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(matches) != 2 {
			t.Error("typed predicate composition changed meaning")
		}
		found, err := base.Where(fields.Age.Gte(30)).Find(t.Context(), tx, secondID)
		if err != nil {
			return err
		}
		if v, ok := found.Get(); !ok || v.ID != secondID {
			t.Error("typed Find lost filters or identity")
		}
		missing, err := base.Where(fields.Age.Lt(30)).Find(t.Context(), tx, secondID)
		if err != nil {
			return err
		}
		if missing.IsSet() {
			t.Error("missing model used a zero model sentinel")
		}
		if _, err := base.Where(fields.Age.Lt(0)).RequireFirst(t.Context(), tx); !errors.Is(err, database.NotFound) {
			t.Error("required missing model did not report NotFound")
		}
		page := base.Limit(1).Offset(1)
		pageUsers, err := page.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(pageUsers) != 1 || pageUsers[0].ID != secondID {
			t.Error("offset window incorrect")
		}
		for _, item := range []struct {
			q      models.UserQuery
			count  int64
			exists bool
		}{{base, 2, true}, {page, 1, true}, {base.Limit(0), 0, false}, {base.Offset(2), 0, false}} {
			count, err := item.q.Count(t.Context(), tx)
			if err != nil {
				return err
			}
			exists, err := item.q.Exists(t.Context(), tx)
			if err != nil {
				return err
			}
			if count != item.count || exists != item.exists {
				t.Error("aggregate did not honor selected query window")
			}
		}
		country, err := models.QueryCountries().RequireFind(t.Context(), tx, models.CountryCode("MY"))
		if err != nil {
			return err
		}
		if country.Code != models.CountryCode("MY") || country.Name != "Malaysia" {
			t.Error("natural-key model hydration failed")
		}
		amount, err := decimal.Parse("9007199254740993.123456789012345678901")
		if err != nil {
			return err
		}
		entry, err := models.QueryLedgerEntries().Where(models.LedgerEntryFields().Amount.Eq(amount)).RequireFind(t.Context(), tx, entryID)
		if err != nil {
			return err
		}
		if entry.Amount != amount || !entry.Balance.IsNull() {
			t.Error("model decimal precision or NULL changed")
		}
		stop := errors.New("stop streaming")
		seen := 0
		if err := base.Each(t.Context(), tx, func(models.User) error { seen++; return stop }); !errors.Is(err, stop) || seen != 1 {
			t.Error("stream early exit failed")
		}
		if _, err := base.Count(t.Context(), tx); err != nil {
			return err
		}
		if _, err := base.Where(fields.Status.Eq(models.Status("unknown"))).All(t.Context(), tx); !errors.Is(err, fault.Invalid) {
			t.Error("invalid enum binding reached execution")
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := base.All(canceled, tx); !errors.Is(err, context.Canceled) {
			t.Error("canceled model query executed")
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='unknown' WHERE id=$1`, secondID.String()); err != nil {
			return err
		}
		if partial, err := base.All(t.Context(), tx); err == nil || partial != nil {
			t.Error("malformed row published a partial model collection")
		}
		if partial, err := base.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1}); err == nil || partial.Items != nil {
			t.Error("malformed lookahead row published a cursor page")
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='disabled' WHERE id=$1`, secondID.String()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedModelSQLUsesOnlyDeclaredColumns(t *testing.T) {
	statement, err := models.QueryUsers().Where(models.UserFields().Email.Eq("secret'; --")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statement.SQL(), "Scratch") || strings.Contains(statement.SQL(), "secret") || !strings.Contains(statement.SQL(), `"users"."email_address"`) {
		t.Fatal("generated SQL lost column declarations or bound values")
	}
}
