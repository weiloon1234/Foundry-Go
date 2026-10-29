package consumer_test

import "time"

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedModelReads(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	firstID, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	entryID, err := model.NewIDAt[models.LedgerEntry](time.Now())
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

		fields := models.Users
		base := models.Users.Query().OrderBy(orm.NumberAsc(fields.Age))
		users, err := orm.All(t.Context(), tx, base)
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
		byBirthday := models.Users.Query().OrderBy(orm.NullableTimeAsc(fields.Birthday))
		cursorPage, err := orm.CursorPaginate(t.Context(), tx, byBirthday, orm.CursorRequest[models.User]{Size: 1})
		if err != nil {
			return err
		}
		nextCursor, ok := cursorPage.Next.Get()
		if !ok || len(cursorPage.Items) != 1 || cursorPage.Items[0].ID != firstID {
			return errors.New("nullable date cursor did not start at the non-null row")
		}
		lastPage, err := orm.CursorPaginate(t.Context(), tx, byBirthday, orm.CursorRequest[models.User]{Size: 1, After: value.Set(nextCursor)})
		if err != nil {
			return err
		}
		if len(lastPage.Items) != 1 || lastPage.Items[0].ID != secondID || lastPage.Next.IsSet() {
			t.Error("temporal/UUID cursor lost its exact boundary")
		}
		match, err := orm.Require(t.Context(), tx, base.Where(orm.TextContains(fields.Email, "_%!'")))
		if err != nil {
			return err
		}
		if match.ID != firstID {
			t.Error("Contains treated literal wildcard characters as operators")
		}
		matches, err := orm.All(t.Context(), tx, base.Where(orm.Or(orm.NumberLt(fields.Age, 22), orm.EnumEq(fields.Status, models.StatusDisabled)), orm.KeyIn(fields.ID, firstID, secondID)))
		if err != nil {
			return err
		}
		if len(matches) != 2 {
			t.Error("typed predicate composition changed meaning")
		}
		found, err := orm.First(t.Context(), tx, base.Where(orm.NumberGte(fields.Age, 30), orm.KeyEq(fields.ID, secondID)))
		if err != nil {
			return err
		}
		if v, ok := found.Get(); !ok || v.ID != secondID {
			t.Error("typed Find lost filters or identity")
		}
		missing, err := orm.First(t.Context(), tx, base.Where(orm.NumberLt(fields.Age, 30), orm.KeyEq(fields.ID, secondID)))
		if err != nil {
			return err
		}
		if missing.IsSet() {
			t.Error("missing model used a zero model sentinel")
		}
		if _, err := orm.Require(t.Context(), tx, base.Where(orm.NumberLt(fields.Age, 0))); !errors.Is(err, database.NotFound) {
			t.Error("required missing model did not report NotFound")
		}
		page := base.Limit(1).Offset(1)
		pageUsers, err := orm.All(t.Context(), tx, page)
		if err != nil {
			return err
		}
		if len(pageUsers) != 1 || pageUsers[0].ID != secondID {
			t.Error("offset window incorrect")
		}
		for _, item := range []struct {
			q      orm.Query[models.User]
			count  int64
			exists bool
		}{{base, 2, true}, {page, 1, true}, {base.Limit(0), 0, false}, {base.Offset(2), 0, false}} {
			count, err := orm.Count(t.Context(), tx, item.q)
			if err != nil {
				return err
			}
			exists, err := orm.Exists(t.Context(), tx, item.q)
			if err != nil {
				return err
			}
			if count != item.count || exists != item.exists {
				t.Error("aggregate did not honor selected query window")
			}
		}
		country, err := orm.RequireKey(t.Context(), tx, models.Countries.Table, models.CountryCode("MY"))
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
		entry, err := orm.Require(t.Context(), tx, models.LedgerEntries.Query().Where(orm.NumberEq(models.LedgerEntries.Amount, amount), orm.KeyEq(models.LedgerEntries.Key(), entryID)))
		if err != nil {
			return err
		}
		if entry.Amount != amount || !entry.Balance.IsNull() {
			t.Error("model decimal precision or NULL changed")
		}
		stop := errors.New("stop streaming")
		seen := 0
		if err := orm.Each(t.Context(), tx, base, func(models.User) error { seen++; return stop }); !errors.Is(err, stop) || seen != 1 {
			t.Error("stream early exit failed")
		}
		if _, err := orm.Count(t.Context(), tx, base); err != nil {
			return err
		}
		if _, err := orm.All(t.Context(), tx, base.Where(orm.EnumEq(fields.Status, models.Status("unknown")))); !errors.Is(err, fault.Invalid) {
			t.Error("invalid enum binding reached execution")
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := orm.All(canceled, tx, base); !errors.Is(err, context.Canceled) {
			t.Error("canceled model query executed")
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='unknown' WHERE id=$1`, secondID.String()); err != nil {
			return err
		}
		if partial, err := orm.All(t.Context(), tx, base); err == nil || partial != nil {
			t.Error("malformed row published a partial model collection")
		}
		if partial, err := orm.CursorPaginate(t.Context(), tx, base, orm.CursorRequest[models.User]{Size: 1}); err == nil || partial.Items != nil {
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

func TestORMModelSQLUsesOnlyDeclaredColumns(t *testing.T) {
	statement, err := orm.Compile(models.Users.Query().Where(orm.TextEq(models.Users.Email, "secret'; --")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statement.SQL(), "Scratch") || strings.Contains(statement.SQL(), "secret") || !strings.Contains(statement.SQL(), `"users"."email_address"`) {
		t.Fatal("generated SQL lost column declarations or bound values")
	}
}
