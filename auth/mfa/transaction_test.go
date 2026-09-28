package mfa

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	lockoutmemory "github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type factorSubject struct {
	ID       int64
	Password password.Hash
	MFA      bool
}

func (m factorSubject) reference() model.Reference[factorSubject, int64] {
	return model.NewReference[factorSubject]("factor_subjects", m.ID, codec.Signed[int64]())
}
func (m factorSubject) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

type protocolBackend struct {
	mode string
	now  temporal.DateTime
	seen *Record
}

func (b *protocolBackend) Prune(context.Context, Address, int) (uint64, error) { return 0, nil }
func (b *protocolBackend) Within(ctx context.Context, address Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error) {
	tx := &database.Tx{}
	if b.mode == "omit" {
		return value.Optional[Record]{}, nil
	}
	if b.mode == "out-of-order" {
		_, _ = change(ctx, tx, value.Optional[Record]{}, b.now)
	}
	if err := prepare(ctx, tx); err != nil && b.mode != "out-of-order" {
		return value.Optional[Record]{}, err
	}
	if b.mode == "repeat-prepare" {
		_ = prepare(ctx, tx)
	}
	if b.mode == "omit-change" {
		return value.Optional[Record]{}, nil
	}
	used := tx
	if b.mode == "different-tx" {
		used = &database.Tx{}
	}
	mutation, err := change(ctx, used, value.Optional[Record]{}, b.now)
	if err != nil && b.mode != "suppress" {
		return value.Optional[Record]{}, err
	}
	if b.mode == "repeat-change" {
		_, _ = change(ctx, used, value.Optional[Record]{}, b.now)
	}
	if b.mode == "discard" {
		return value.Optional[Record]{}, nil
	}
	if record, present := mutation.Next().Get(); present {
		if b.seen != nil {
			*b.seen = record.Clone()
		}
		if b.mode == "different-record" {
			record.ID, _ = model.NewID[Record]()
		}
		return value.Set(record), nil
	}
	return value.Optional[Record]{}, nil
}

func TestMFATransactionRejectsOmittedRepeatedOrSuppressedCallbacks(t *testing.T) {
	config := password.DefaultConfig()
	config.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
	hasher, err := password.New(config)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := password.NewPlaintext(secret.New("private factor password"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	subject := factorSubject{ID: 7, Password: hash}
	provider := auth.DefineProvider("factor.subjects", subject.reference(), func(context.Context, int64) (value.Optional[factorSubject], error) {
		panic("unexpected provider lookup")
	}, func(context.Context, factorSubject) (bool, error) { return true, nil })
	login, err := auth.NewPasswordLogin(provider, hasher, auth.PasswordModel[factorSubject, string]{
		Lookup: func(context.Context, string) (value.Optional[factorSubject], error) { return value.Set(subject), nil },
		Lock: func(context.Context, *database.Tx, factorSubject) (value.Optional[factorSubject], error) {
			return value.Set(subject), nil
		},
		Hash: func(m factorSubject) password.Hash { return m.Password },
		Rehash: func(context.Context, factorSubject, password.Hash, password.Hash) (value.Optional[factorSubject], error) {
			panic("unexpected rehash")
		},
		RequiresMFA: func(context.Context, factorSubject) (bool, error) { return false, nil },
	}, auth.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := login.Authenticate(t.Context(), "member", plain)
	if err != nil {
		t.Fatal(err)
	}
	source := testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	now, err := temporal.NewDateTime(source.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, err := encryption.GenerateKey("test")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring(key.ID(), key)
	if err != nil {
		t.Fatal(err)
	}
	namespace := keyspace.Namespace{Application: "mfa-runtime-tests", Environment: "test"}
	for _, mode := range []string{"valid", "omit", "omit-change", "out-of-order", "repeat-prepare", "repeat-change", "different-tx", "discard", "different-record", "suppress-error", "suppress-panic", "suppress-goexit"} {
		t.Run(mode, func(t *testing.T) {
			backendMode := mode
			if len(mode) > 8 && mode[:8] == "suppress" {
				backendMode = "suppress"
			}
			var stored Record
			backend := &protocolBackend{mode: backendMode, now: now, seen: &stored}
			store, err := NewStore(backend, keys, DefaultConfig(namespace, "Foundry"))
			if err != nil {
				t.Fatal(err)
			}
			memory, err := lockoutmemory.New(16, source)
			if err != nil {
				t.Fatal(err)
			}
			attempts, err := lockout.NewStore(memory, lockout.DefaultConfig(namespace))
			if err != nil {
				t.Fatal(err)
			}
			throttle, err := lockout.Define("mfa.subjects", keyspace.SignedKeys[int64](), lockout.DefaultPolicy()).Bind(attempts)
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("account label failed")
			factors, err := New(store, provider, Model[factorSubject, int64]{
				Lock: func(context.Context, *database.Tx, int64) (value.Optional[factorSubject], error) {
					return value.Set(subject), nil
				},
				Enabled: func(m factorSubject) bool { return m.MFA },
				SetEnabled: func(_ context.Context, _ *database.Tx, m factorSubject, enabled bool) (factorSubject, error) {
					m.MFA = enabled
					return m, nil
				},
				AccountLabel: func(factorSubject) (string, error) {
					switch mode {
					case "suppress-error":
						return "", cause
					case "suppress-panic":
						panic("private label")
					case "suppress-goexit":
						runtime.Goexit()
					}
					return "member@example.test", nil
				},
				CanDisable: func(context.Context, factorSubject) (bool, error) { return true, nil },
				Invalidate: func(context.Context, *database.Tx, factorSubject) error { return nil },
			}, throttle)
			if err != nil {
				t.Fatal(err)
			}
			enrollment, err := factors.Enroll(t.Context(), result)
			if mode == "valid" {
				if err != nil || enrollment.ID().IsZero() || !stored.PendingUntil.IsSet() {
					t.Fatal("valid backend rejected", err)
				}
				owner, err := stored.encryptionContext()
				if err != nil {
					t.Fatal(err)
				}
				raw, err := keys.Decrypt(t.Context(), owner, stored.Ciphertext)
				if err != nil || raw != enrollment.Secret().Secret() {
					t.Fatal("factor encryption binding", err)
				}
			} else {
				if err == nil || !enrollment.ID().IsZero() || !enrollment.URI().IsZero() {
					t.Fatal("invalid backend published enrollment")
				}
				if mode == "suppress-error" && !errors.Is(err, cause) {
					t.Fatal("suppressed cause lost", err)
				}
			}
		})
	}
}
