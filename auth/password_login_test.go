package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type loginEmail string
type passwordSubject struct {
	ID           int64
	Digest       password.Hash
	Enabled, MFA bool
}

func (s passwordSubject) FoundryReference() model.Reference[passwordSubject, int64] {
	return model.NewReference[passwordSubject]("password_subjects", s.ID, codec.Signed[int64]())
}
func (s passwordSubject) FoundryIdentity() (model.Identity, error) {
	return s.FoundryReference().Identity()
}
func loginHasher(t *testing.T, iterations uint32) *password.Hasher {
	t.Helper()
	c := password.DefaultConfig()
	c.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: iterations, Parallelism: 1}
	h, err := password.New(c)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func loginPlain(t *testing.T, text string) password.Plaintext {
	t.Helper()
	p, err := password.NewPlaintext(secret.New(text))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func passwordProvider(eligible func(context.Context, passwordSubject) (bool, error)) auth.Provider[passwordSubject, int64] {
	return auth.DefineProvider("password.subjects", (passwordSubject{}).FoundryReference(), func(context.Context, int64) (value.Optional[passwordSubject], error) {
		panic("password login performed a second model lookup")
	}, eligible)
}
func loginBinding(s passwordSubject) auth.PasswordModel[passwordSubject, loginEmail] {
	return auth.PasswordModel[passwordSubject, loginEmail]{
		Lock: func(_ context.Context, _ *database.Tx, subject passwordSubject) (value.Optional[passwordSubject], error) {
			return value.Set(subject), nil
		},
		Lookup: func(_ context.Context, key loginEmail) (value.Optional[passwordSubject], error) {
			if key != "member@example.test" {
				return value.Optional[passwordSubject]{}, nil
			}
			return value.Set(s), nil
		},
		Hash: func(s passwordSubject) password.Hash { return s.Digest },
		Rehash: func(_ context.Context, subject passwordSubject, old, next password.Hash) (value.Optional[passwordSubject], error) {
			if old != s.Digest {
				return value.Optional[passwordSubject]{}, nil
			}
			subject.Digest = next
			return value.Set(subject), nil
		},
		RequiresMFA: func(_ context.Context, s passwordSubject) (bool, error) { return s.MFA, nil },
	}
}
func loginInstance(t *testing.T, h *password.Hasher, b auth.PasswordModel[passwordSubject, loginEmail], configs ...auth.Config) *auth.PasswordLogin[passwordSubject, int64, loginEmail] {
	t.Helper()
	config := auth.DefaultConfig()
	if len(configs) > 0 {
		config = configs[0]
	}
	l, err := auth.NewPasswordLogin(passwordProvider(func(_ context.Context, s passwordSubject) (bool, error) { return s.Enabled, nil }), h, b, config)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func assertNoPasswordAuthority(t *testing.T, result auth.PasswordResult[passwordSubject, int64]) {
	t.Helper()
	if !result.Proof().Identity().IsZero() || result.Proof().Assurance() != 0 || result.Subject() != (passwordSubject{}) {
		t.Fatal("failed login retained model or proof")
	}
}

func TestPasswordLoginReturnsConcreteModelWithExplicitAssurance(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "private password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, mfa := range []bool{false, true} {
		s := passwordSubject{ID: 7, Digest: hash, Enabled: true, MFA: mfa}
		b := loginBinding(s)
		lookups, checks := 0, 0
		lookup := b.Lookup
		b.Lookup = func(ctx context.Context, key loginEmail) (value.Optional[passwordSubject], error) {
			lookups++
			return lookup(ctx, key)
		}
		b.Rehash = func(context.Context, passwordSubject, password.Hash, password.Hash) (value.Optional[passwordSubject], error) {
			t.Error("current hash was rewritten")
			return value.Optional[passwordSubject]{}, nil
		}
		p := passwordProvider(func(_ context.Context, got passwordSubject) (bool, error) { checks++; return got.Enabled, nil })
		l, err := auth.NewPasswordLogin(p, h, b, auth.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		result, err := l.Authenticate(t.Context(), "member@example.test", plain)
		if err != nil {
			t.Fatal(err)
		}
		state := auth.Authenticated
		if mfa {
			state = auth.PendingMFA
		}
		expected, _ := s.FoundryIdentity()
		if result.Subject() != s || result.Proof().Identity() != expected || result.Proof().Assurance() != state || lookups != 1 || checks != 1 {
			t.Fatal("wrong model, proof or duplicate hydration")
		}
		encoded, err := json.Marshal(result)
		if err != nil || string(encoded) != "{}" {
			t.Fatal("login result became a public DTO", err)
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", result, result), "argon2") {
			t.Fatal("login result disclosed hash")
		}
	}
}

func TestPasswordLoginUniformCredentialRejection(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "right password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "mismatch", "disabled", "unusable", "over-budget"} {
		t.Run(mode, func(t *testing.T) {
			subject := passwordSubject{ID: 7, Digest: hash, Enabled: true}
			key := loginEmail("member@example.test")
			submitted := plain
			switch mode {
			case "missing":
				key = "missing@example.test"
			case "mismatch":
				submitted = loginPlain(t, "wrong password")
			case "disabled":
				subject.Enabled = false
			case "unusable":
				subject.Digest = password.Hash{}
			case "over-budget":
				var err error
				subject.Digest, err = password.ParseHash(secret.New(strings.Replace(hash.Encoded().Reveal(), "m=19456", "m=262144", 1)))
				if err != nil {
					t.Fatal(err)
				}
			}
			b := loginBinding(subject)
			b.RequiresMFA = func(context.Context, passwordSubject) (bool, error) {
				t.Error("rejected credential reached factor policy")
				return false, nil
			}
			result, err := loginInstance(t, h, b).Authenticate(t.Context(), key, submitted)
			if !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("nonuniform credential rejection", err)
			}
			assertNoPasswordAuthority(t, result)
		})
	}
}

func TestPasswordLoginRehashValidatesCASResultAndPreservesFailures(t *testing.T) {
	oldHasher, h := loginHasher(t, 2), loginHasher(t, 3)
	plain := loginPlain(t, "right password")
	old, err := oldHasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	original := passwordSubject{ID: 7, Digest: old, Enabled: true}
	failure := errors.New("private uncertain write")
	for _, mode := range []string{"updated", "lost", "identity", "hash", "disabled", "error"} {
		t.Run(mode, func(t *testing.T) {
			b := loginBinding(original)
			writes := 0
			b.Rehash = func(ctx context.Context, s passwordSubject, expected, next password.Hash) (value.Optional[passwordSubject], error) {
				writes++
				if expected != old || s != original || next == old {
					return value.Optional[passwordSubject]{}, errors.New("CAS lost its original values")
				}
				s.Digest = next
				switch mode {
				case "lost":
					return value.Optional[passwordSubject]{}, nil
				case "identity":
					s.ID++
				case "hash":
					s.Digest = old
				case "disabled":
					s.Enabled = false
				case "error":
					return value.Set(s), failure
				}
				return value.Set(s), nil
			}
			result, err := loginInstance(t, h, b).Authenticate(t.Context(), "member@example.test", plain)
			if writes != 1 {
				t.Fatal("CAS retried or not called")
			}
			switch mode {
			case "updated":
				if err != nil || result.Subject().Digest == old {
					t.Fatal("rehash failed", err)
				}
				if needs, err := h.NeedsRehash(result.Subject().Digest); err != nil || needs {
					t.Fatal("rehash did not apply issuance policy", err)
				}
			case "lost", "disabled":
				if !errors.Is(err, auth.Unauthenticated) {
					t.Fatal("stale or disabled model accepted", err)
				}
				assertNoPasswordAuthority(t, result)
			case "error":
				if !errors.Is(err, failure) || strings.Contains(err.Error(), "private") {
					t.Fatal("uncertain write error leaked or was lost", err)
				}
				assertNoPasswordAuthority(t, result)
			default:
				if !errors.Is(err, fault.Invalid) {
					t.Fatal("corrupt rehash result accepted", err)
				}
				assertNoPasswordAuthority(t, result)
			}
		})
	}
}

func TestPasswordLoginCallbackFailuresAndCapacity(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "right password")
	for _, mode := range []string{"panic", "goexit", "error"} {
		b := loginBinding(passwordSubject{})
		private := errors.New("private lookup value")
		b.Lookup = func(context.Context, loginEmail) (value.Optional[passwordSubject], error) {
			switch mode {
			case "panic":
				panic(private)
			case "goexit":
				runtime.Goexit()
			}
			return value.Optional[passwordSubject]{}, private
		}
		l := loginInstance(t, h, b)
		for range 2 {
			result, err := l.Authenticate(t.Context(), "member@example.test", plain)
			assertNoPasswordAuthority(t, result)
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("callback failure leaked")
			}
			if mode != "error" && !errors.Is(err, fault.Panicked) {
				t.Fatal("callback exit not classified", err)
			}
		}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	b := loginBinding(passwordSubject{})
	b.Lookup = func(context.Context, loginEmail) (value.Optional[passwordSubject], error) {
		close(entered)
		<-release
		return value.Optional[passwordSubject]{}, nil
	}
	config := auth.DefaultConfig()
	config.MaxConcurrent = 1
	l := loginInstance(t, h, b, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := l.Authenticate(ctx, "member@example.test", plain)
		if !result.Proof().Identity().IsZero() {
			err = errors.New("canceled login returned proof")
		}
		done <- err
	}()
	<-entered
	cancel()
	result, err := l.Authenticate(t.Context(), "another@example.test", plain)
	if !errors.Is(err, fault.Conflict) {
		t.Error("active canceled lookup released its slot", err)
	}
	assertNoPasswordAuthority(t, result)
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestPasswordLoginRejectsIncompleteBindingsAndTimeout(t *testing.T) {
	h := loginHasher(t, 2)
	p := passwordProvider(func(context.Context, passwordSubject) (bool, error) { return true, nil })
	if _, err := auth.NewPasswordLogin(p, h, auth.PasswordModel[passwordSubject, loginEmail]{}, auth.DefaultConfig()); err == nil {
		t.Fatal("incomplete model accepted")
	}
	b := loginBinding(passwordSubject{})
	b.RequiresMFA = nil
	if _, err := auth.NewPasswordLogin(p, h, b, auth.DefaultConfig()); err == nil {
		t.Fatal("implicit MFA policy accepted")
	}
	b = loginBinding(passwordSubject{})
	b.Lookup = func(ctx context.Context, _ loginEmail) (value.Optional[passwordSubject], error) {
		<-ctx.Done()
		return value.Optional[passwordSubject]{}, nil
	}
	c := auth.DefaultConfig()
	c.Timeout = time.Millisecond
	result, err := loginInstance(t, h, b, c).Authenticate(t.Context(), "member@example.test", loginPlain(t, "right password"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline lost", err)
	}
	assertNoPasswordAuthority(t, result)
	var nilLogin *auth.PasswordLogin[passwordSubject, int64, loginEmail]
	if _, err := nilLogin.Authenticate(t.Context(), "", password.Plaintext{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil login accepted", err)
	}
}
