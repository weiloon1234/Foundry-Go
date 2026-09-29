package postgres_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/internal/mfastore"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func key(t *testing.T, id encryption.KeyID) encryption.Key {
	t.Helper()
	k, err := encryption.GenerateKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func ring(t *testing.T, active encryption.KeyID, keys ...encryption.Key) *encryption.Keyring {
	t.Helper()
	r, err := encryption.NewKeyring(active, keys...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// storedFactor mirrors the MFA binding: purpose plus opaque subject key and
// factor generation. It is duplicated here only to seed encrypted rows.
func storedFactor(t *testing.T, keys *encryption.Keyring, owner string, generation model.ID[mfastore.Factor]) string {
	t.Helper()
	binding, err := encryption.NewContext("auth.mfa.totp.v1", secret.New(owner+":"+generation.String()))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := keys.Encrypt(t.Context(), binding, secret.New("JBSWY3DPEHPK3PXP"+owner[60:]))
	if err != nil {
		t.Fatal(err)
	}
	return ciphertext.Encoded()
}

// Key rotation re-encrypts every stored factor under the active key in bounded
// batches without passwords or models, skips envelopes whose key is gone, and
// never overwrites a factor that changed concurrently.
func TestFactorKeyRotationReencryptsStoredFactors(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	old, next, gone := key(t, "factors_2025"), key(t, "factors_2026"), key(t, "retired_2024")
	oldRing, newRing, goneRing := ring(t, "factors_2025", old), ring(t, "factors_2026", old, next), ring(t, "retired_2024", gone)
	identity, err := model.NewReference[struct{}]("rotation_members", int64(1), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := value.NewJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := value.NewJSON([]string{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	created, err := temporal.NewDateTime(now)
	if err != nil {
		t.Fatal(err)
	}
	generations := map[string]model.ID[mfastore.Factor]{}
	owners := make([]string, 7)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range mfapg.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		for i := range owners {
			owners[i] = fmt.Sprintf("%064x", i+1)
			generation, err := model.NewID[mfastore.Factor]()
			if err != nil {
				return err
			}
			generations[owners[i]] = generation
			keys := oldRing
			switch i {
			case 5:
				keys = newRing // Already rotated.
			case 6:
				keys = goneRing // Retired key no longer retained.
			}
			draft := mfastore.FactorDraft{}.SetKey(owners[i]).SetScope(strings.Repeat("a", 64)).SetIdentity(stored).SetGeneration(generation).
				SetCiphertext(storedFactor(t, keys, owners[i], generation)).SetCreatedAt(created).SetConfirmedAt(created).SetLastStep(1).SetRecoveryHashes(hashes).ClearPendingUntil()
			if _, err := mfastore.QueryFoundryMfaFactors().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config := mfapg.DefaultConfig()
	config.Schema = schema
	backend, err := mfapg.New(db, config)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mfa.NewStore(backend, newRing, mfa.DefaultConfig(keyspace.Namespace{Application: "rotation", Environment: "test"}, "Rotation"))
	if err != nil {
		t.Fatal(err)
	}
	var total mfa.Rotation
	cursor := mfa.RotationCursor{}
	for batches := 0; ; batches++ {
		if batches > 10 {
			t.Fatal("rotation did not finish")
		}
		batch, err := store.ReencryptStale(t.Context(), cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		total.Reencrypted += batch.Reencrypted
		total.Failed += batch.Failed
		total.Changed += batch.Changed
		cursor = batch.Next
		if batch.Done {
			break
		}
	}
	if total.Reencrypted != 5 || total.Failed != 1 || total.Changed != 0 {
		t.Fatal("unexpected rotation outcome", total)
	}
	prefix, _ := encryption.EnvelopePrefix("factors_2026")
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		rows, err := mfastore.QueryFoundryMfaFactors().All(t.Context(), tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Key == owners[6] {
				if strings.HasPrefix(row.Ciphertext, prefix) {
					t.Error("undecryptable factor was rewritten")
				}
				continue
			}
			if !strings.HasPrefix(row.Ciphertext, prefix) || row.Generation != generations[row.Key] {
				t.Error("factor was not rotated in place", row.Key)
			}
			binding, _ := encryption.NewContext("auth.mfa.totp.v1", secret.New(row.Key+":"+row.Generation.String()))
			ciphertext, err := encryption.ParseCiphertext(row.Ciphertext)
			if err != nil {
				return err
			}
			plain, err := newRing.Decrypt(t.Context(), binding, ciphertext)
			if err != nil || plain.Reveal() != "JBSWY3DPEHPK3PXP"+row.Key[60:] {
				t.Error("rotated factor lost its secret", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	again, err := store.ReencryptStale(t.Context(), mfa.RotationCursor{}, mfa.MaxRotationBatch)
	if err != nil || again.Reencrypted != 0 || again.Failed != 1 || !again.Done {
		t.Fatal("second pass was not idempotent", again, err)
	}
	stale, err := backend.StaleCiphertexts(t.Context(), "factors_2026", "", 10)
	if err != nil || len(stale) != 1 {
		t.Fatal(len(stale), err)
	}
	changed := stale[0]
	changed.Generation, _ = model.NewID[mfa.Record]()
	if replaced, err := backend.ReplaceCiphertext(t.Context(), changed, stale[0].Ciphertext); err != nil || replaced {
		t.Fatal("replacement ignored the factor generation", replaced, err)
	}
	if _, err := store.ReencryptStale(t.Context(), mfa.RotationCursor{}, mfa.MaxRotationBatch+1); err == nil {
		t.Fatal("unbounded rotation batch accepted")
	}
}
