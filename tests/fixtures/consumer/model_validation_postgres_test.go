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

// Check-time scopes and batched element lookups run against real PostgreSQL:
// one declaration serves several requests and valid lists use one statement.
func TestPostgresCheckTimeScopesAndElementLookups(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	var users [3]model.ID[models.User]
	for i := range users {
		id, err := model.NewID[models.User]()
		if err != nil {
			t.Fatal(err)
		}
		users[i] = id
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text UNIQUE NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO users (id,email_address,age,status,level) VALUES ($1,'first@example.test',21,'active',1),($2,'second@example.test',22,'disabled',1),($3,'third@example.test',23,'active',1)`, users[0].String(), users[1].String(), users[2].String()); err != nil {
			return err
		}
		executor := &readOnlyValidationExecutor{Executor: tx}
		type update struct {
			ID    model.ID[models.User]
			Email string
		}
		current := validation.NewSlot[model.ID[models.User]]()
		email := validation.DefineField("email", func(input update) string { return input.Email })
		rename := validation.Provide(current, func(_ context.Context, input update) (model.ID[models.User], error) { return input.ID, nil },
			email.Rules(modelvalidation.AvailableEmailExceptCurrent(executor, current)))
		if err := rename.Validate(); err != nil {
			return err
		}
		if err := rename.Check(t.Context(), update{ID: users[0], Email: "first@example.test"}, validation.DefaultLimits()); err != nil {
			return err
		}
		var rejected *validation.Errors
		if err := rename.Check(t.Context(), update{ID: users[1], Email: "first@example.test"}, validation.DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Path != "/email" || rejected.Issues()[0].Code != "foundry.unique" {
			return fmt.Errorf("unique-ignoring rejection: %v", err)
		}
		if modelvalidation.AvailableEmailExceptCurrent(executor, current).Validate() == nil {
			return errors.New("slot reader validated without a provider")
		}

		status := validation.NewSlot[models.Status]()
		byStatus := validation.Provide(status, func(context.Context, model.ID[models.User]) (models.Status, error) { return models.StatusDisabled, nil },
			modelvalidation.UserInStatus(executor, status))
		if err := byStatus.Check(t.Context(), users[1], validation.DefaultLimits()); err != nil {
			return err
		}
		if err := byStatus.Check(t.Context(), users[0], validation.DefaultLimits()); !errors.As(err, &rejected) {
			return fmt.Errorf("scoped lookup accepted another status: %v", err)
		}

		assignments := modelvalidation.ActiveAssignees(executor)
		before := executor.queries
		valid := []modelvalidation.Assignment{{Assignee: users[0]}, {Assignee: users[2]}, {Assignee: users[0]}}
		if err := assignments.Check(t.Context(), valid, validation.DefaultLimits()); err != nil || executor.queries != before+1 {
			return fmt.Errorf("valid assignments used %d statements: %v", executor.queries-before, err)
		}
		invalid := []modelvalidation.Assignment{{Assignee: users[0]}, {Assignee: users[2]}, {Assignee: users[1]}, {Assignee: users[0]}}
		if err := assignments.Check(t.Context(), invalid, validation.DefaultLimits()); !errors.As(err, &rejected) || len(rejected.Issues()) != 1 || rejected.Issues()[0].Path != "/2/assignee_id" {
			return fmt.Errorf("element path rejection: %v", err)
		}
		emails := databasevalidation.UniqueAll(executor, models.QueryUsers(), models.UserFields().Email)
		before = executor.queries
		if err := emails.Check(t.Context(), []string{"new@example.test", "other@example.test"}, validation.DefaultLimits()); err != nil || executor.queries != before+1 {
			return fmt.Errorf("unique list used %d statements: %v", executor.queries-before, err)
		}
		if err := emails.Check(t.Context(), []string{"new@example.test", "third@example.test"}, validation.DefaultLimits()); !errors.As(err, &rejected) || rejected.Issues()[0].Path != "/1" || rejected.Issues()[0].Code != "foundry.unique_all" {
			return fmt.Errorf("unique list rejection: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
