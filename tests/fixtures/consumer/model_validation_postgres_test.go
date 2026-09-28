package consumer_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/modelvalidation"
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

type readOnlyValidationExecutor struct {
	database.Executor
	queries int
}

func (e *readOnlyValidationExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	return database.Result{}, errors.New("validation must not execute writes")
}
func (e *readOnlyValidationExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	e.queries++
	if !strings.HasPrefix(sql, "SELECT EXISTS(") {
		return nil, errors.New("validation did not use model existence execution")
	}
	return e.Executor.Query(ctx, sql, args...)
}

func TestPostgresTypedModelValidation(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	first, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := model.NewID[models.LedgerEntry]()
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text UNIQUE NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
			`CREATE TABLE countries (code text PRIMARY KEY, name text NOT NULL)`,
			`CREATE TABLE ledger_entries (id uuid PRIMARY KEY, amount numeric NOT NULL, balance numeric)`,
			`CREATE TABLE soft_groups (code text PRIMARY KEY, name text NOT NULL, deleted_at timestamptz)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		const storedEmail = "literal_%!'@example.test"
		if _, err := tx.Exec(t.Context(), `INSERT INTO users (id,email_address,age,nickname,status,level) VALUES ($1,$2,21,'member','active',1),($3,'disabled@example.test',35,NULL,'disabled',2)`, first.String(), storedEmail, second.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO countries (code,name) VALUES ('MY','Malaysia')`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO soft_groups (code,name,deleted_at) VALUES ('deleted','reserved',CURRENT_TIMESTAMP)`); err != nil {
			return err
		}
		const exact = "9007199254740993.123456789012345678901"
		if _, err := tx.Exec(t.Context(), `INSERT INTO ledger_entries (id,amount) VALUES ($1,$2)`, entry.String(), exact); err != nil {
			return err
		}
		executor := &readOnlyValidationExecutor{Executor: tx}
		checkRejected := func(err error, code string) error {
			var rejected *validation.Errors
			if !errors.As(err, &rejected) || len(rejected.Issues()) != 1 || string(rejected.Issues()[0].Code) != code {
				return fmt.Errorf("expected %s rejection, got %v", code, err)
			}
			return nil
		}
		unique := modelvalidation.AvailableEmail(executor)
		if executor.queries != 0 {
			return errors.New("declaration queried the database")
		}
		if err := checkRejected(unique.Check(t.Context(), storedEmail, validation.DefaultLimits()), "foundry.unique"); err != nil {
			return err
		}
		if err := unique.Check(t.Context(), "free@example.test", validation.DefaultLimits()); err != nil {
			return err
		}
		if err := modelvalidation.AvailableEmailForUpdate(executor, first).Check(t.Context(), storedEmail, validation.DefaultLimits()); err != nil {
			return err
		}
		if err := checkRejected(modelvalidation.AvailableEmailForUpdate(executor, second).Check(t.Context(), storedEmail, validation.DefaultLimits()), "foundry.unique"); err != nil {
			return err
		}
		active := modelvalidation.ActiveUser(executor)
		if err := active.Check(t.Context(), first, validation.DefaultLimits()); err != nil {
			return err
		}
		if err := checkRejected(active.Check(t.Context(), second, validation.DefaultLimits()), "foundry.exists"); err != nil {
			return err
		}
		if err := databasevalidation.Exists(executor, models.QueryCountries(), models.CountryFields().Code).Check(t.Context(), models.CountryCode("MY"), validation.DefaultLimits()); err != nil {
			return err
		}
		amount, err := decimal.Parse(exact)
		if err != nil {
			return err
		}
		if err := databasevalidation.Exists(executor, models.QueryLedgerEntries(), models.LedgerEntryFields().Amount).Check(t.Context(), amount, validation.DefaultLimits()); err != nil {
			return err
		}
		if err := databasevalidation.Exists(executor, models.QueryUsers(), models.UserFields().Nickname).Check(t.Context(), "member", validation.DefaultLimits()); err != nil {
			return err
		}
		groups := softqueries.QuerySoftGroups()
		if err := databasevalidation.Unique(executor, groups, softqueries.GroupFields().Name).Check(t.Context(), "reserved", validation.DefaultLimits()); err != nil {
			return err
		}
		if err := checkRejected(databasevalidation.Unique(executor, groups.WithTrashed(), softqueries.GroupFields().Name).Check(t.Context(), "reserved", validation.DefaultLimits()), "foundry.unique"); err != nil {
			return err
		}
		if executor.queries != 11 {
			return fmt.Errorf("unexpected lookup count %d", executor.queries)
		}

		batch := modelvalidation.ActiveUsers(executor)
		if err := batch.Check(t.Context(), nil, validation.DefaultLimits()); err != nil || executor.queries != 11 {
			return errors.New("empty batch performed IO or failed")
		}
		ids := make([]model.ID[models.User], 130)
		for i := range ids {
			ids[i] = first
		}
		if err := batch.Check(t.Context(), ids, validation.DefaultLimits()); err != nil {
			return err
		}
		if executor.queries != 14 {
			return errors.New("batch lookup used per-item IO")
		}
		if err := checkRejected(batch.Check(t.Context(), []model.ID[models.User]{first, second}, validation.DefaultLimits()), "foundry.exists_all"); err != nil {
			return err
		}
		// Existing but invisible soft-deleted rows remain inaccessible.
		softBatch := databasevalidation.ExistsAll(executor, groups, softqueries.GroupFields().Name)
		if err := checkRejected(softBatch.Check(t.Context(), []string{"reserved"}, validation.DefaultLimits()), "foundry.exists_all"); err != nil {
			return err
		}
		if err := databasevalidation.ExistsAll(executor, groups.WithTrashed(), softqueries.GroupFields().Name).Check(t.Context(), []string{"reserved", "reserved"}, validation.DefaultLimits()); err != nil {
			return err
		}
		// Stored codecs and SQL equality, including exact decimals, are reused.
		if err := databasevalidation.ExistsAll(executor, models.QueryLedgerEntries(), models.LedgerEntryFields().Amount).Check(t.Context(), []decimal.Decimal{amount, amount}, validation.DefaultLimits()); err != nil {
			return err
		}
		count := executor.queries
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if err := batch.Check(canceled, ids, validation.DefaultLimits()); !errors.Is(err, context.Canceled) {
			return errors.New("batch lost cancellation")
		}
		limits := validation.DefaultLimits()
		limits.Checks = 10
		var bounded *validation.LimitError
		if !errors.As(batch.Check(t.Context(), ids, limits), &bounded) || executor.queries != count {
			return errors.New("over-budget batch performed IO")
		}
		// The transaction remains usable after validation failure/cancellation.
		if err := modelvalidation.ActiveUser(executor).Check(t.Context(), first, validation.DefaultLimits()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestModelValidationRejectsInvalidSourcesWithoutIO(t *testing.T) {
	fields := models.UserFields()
	executor := &readOnlyValidationExecutor{}
	for _, source := range []models.UserQuery{
		{}, models.QueryUsers().Limit(0), models.QueryUsers().Offset(1),
		models.QueryUsers().OrderBy(fields.ID.Asc()),
		models.QueryUsers().WithRelationLimits(query.DefaultRelationLimits()),
	} {
		if databasevalidation.Unique(executor, source, fields.Email).Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
	if databasevalidation.Exists(executor, models.QueryUsers(), query.TextField[models.User, string]{}).Validate() == nil {
		t.Fatal("zero field accepted")
	}
	var missing *database.DB
	if databasevalidation.Exists(missing, models.QueryUsers(), fields.ID).Validate() == nil {
		t.Fatal("nil executor accepted")
	}
	if executor.queries != 0 {
		t.Fatal("invalid declarations performed I/O")
	}
}
