package mfa

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type privateFactorDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateFactorDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private MFA diagnostic")
}
func (e *privateFactorDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestMFATransactionDiagnosticsSurviveCredentialGate(t *testing.T) {
	subject := factorSubject{ID: 7}
	provider := auth.DefineProvider("factor.subjects", subject.reference(), func(context.Context, int64) (value.Optional[factorSubject], error) {
		t.Error("transaction unexpectedly looked up its model")
		return value.Optional[factorSubject]{}, nil
	}, func(context.Context, factorSubject) (bool, error) { return true, nil })
	identity, err := subject.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	address := Address{Namespace: keyspace.Namespace{Application: "mfa-diagnostics", Environment: "test"}, Provider: provider.Name(), Model: provider.ModelName()}
	key, err := encryption.GenerateKey("test")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring(key.ID(), key)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[Record]()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	created, err := temporal.NewDateTime(base)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := temporal.NewDateTime(base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	record := Record{ID: id, Address: address, Subject: identity, CreatedAt: created, PendingUntil: value.Set(pending)}
	owner, err := record.encryptionContext()
	if err != nil {
		t.Fatal(err)
	}
	record.Ciphertext, err = keys.Encrypt(t.Context(), owner, secret.New("private factor"))
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Validate(address, identity); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"future record", "panic formatter", "goexit formatter"} {
		t.Run(stage, func(t *testing.T) {
			cause := &privateFactorDiagnostic{exit: stage == "goexit formatter"}
			factors := &Factors[factorSubject, int64]{store: &Store{gate: credential.NewGate(1, time.Second)}, provider: provider, address: address}
			var original error
			within := func(ctx context.Context, _ Address, _ model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error) {
				tx := &database.Tx{}
				if err := prepare(ctx, tx); err != nil {
					return value.Optional[Record]{}, err
				}
				now := created
				if stage == "future record" {
					var err error
					now, err = temporal.NewDateTime(base.Add(-time.Second))
					if err != nil {
						return value.Optional[Record]{}, err
					}
				}
				_, original = change(ctx, tx, value.Set(record), now)
				return value.Optional[Record]{}, original
			}
			actionCalls := 0
			result := factors.mutate(t.Context(), identity, within, func(context.Context, *database.Tx) (factorSubject, error) { return subject, nil }, func(context.Context, *database.Tx, factorSubject, value.Optional[Record], temporal.DateTime) (Change, error) {
				actionCalls++
				return Change{}, auth.Unauthenticated.WithCause(cause)
			})
			want := "credential operation: unauthenticated"
			if stage == "future record" {
				if actionCalls != 0 {
					t.Fatal("future record reached mutation callback")
				}
			} else {
				want = "credential operation: MFA factor mutation: unauthenticated"
				var retained *privateFactorDiagnostic
				if !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause {
					t.Fatal("MFA mutation lost private cause identity")
				}
			}
			if original == nil || !errors.Is(result, original) || !errors.Is(result, auth.Unauthenticated) {
				t.Fatal("MFA transaction lost source identity or classification")
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != want {
					t.Fatalf("MFA diagnostic = %q, want %q", got, want)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("MFA transaction formatted an application cause")
			}
		})
	}
}
