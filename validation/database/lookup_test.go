package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

type userAccount struct {
	ID    int64
	Email string
}

type userAccountTable struct {
	orm.Table[userAccount, int64]
	Email orm.Text[userAccount, string]
}

func declareUserAccounts() userAccountTable {
	return orm.Define("user_accounts", func(b orm.Builder[userAccount], m *userAccount) userAccountTable {
		return userAccountTable{
			Table: orm.TableOf(b, &m.ID),
			Email: orm.TextOf(b, &m.Email),
		}
	})
}

type stubExecutor struct{}

func (stubExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	return database.Result{}, nil
}

func (stubExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	return nil, nil
}

func TestUniqueIgnoringStaticCouplingAndValidation(t *testing.T) {
	accounts := declareUserAccounts()
	slot := validation.NewSlot[int64]()
	ex := stubExecutor{}

	// Nil key handle is rejected with fault.Invalid
	nilKeyRule := databasevalidation.UniqueIgnoring[userAccount, string, int64](ex, accounts.Query(), orm.TextExpr(accounts.Email), nil, slot)
	if err := validation.ValidateRule(nilKeyRule); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for nil key, got: %v", err)
	}

	// Valid rule requires slot provision before check
	rule := databasevalidation.UniqueIgnoring(ex, accounts.Query(), orm.TextExpr(accounts.Email), orm.KeyExpr(accounts.Key()), slot)
	if err := validation.ValidateRule(rule); err == nil {
		t.Fatal("expected ValidateRule to fail without enclosing Provide")
	}

	type updateInput struct {
		CurrentID int64
		NewEmail  string
	}
	emailField := validation.DefineField("email", func(in updateInput) string { return in.NewEmail })
	provided := validation.Provide(slot, func(_ context.Context, in updateInput) (int64, error) {
		return in.CurrentID, nil
	}, validation.Rules(emailField, rule))

	// With Provide, ValidateRule succeeds
	if err := validation.ValidateRule(provided); err != nil {
		t.Fatalf("expected ValidateRule to succeed with Provide, got: %v", err)
	}
}
