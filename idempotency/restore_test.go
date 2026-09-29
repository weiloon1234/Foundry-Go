package idempotency

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/idempotencystore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func storedTestOutcome(t testing.TB) (Operation[testInput, testOutput], idempotencystore.Record) {
	t.Helper()
	in, out := testCodecs()
	config := DefaultConfig()
	config.MaxResultBytes = 4096
	op := Operation[testInput, testOutput]{store: &Store{config: config}, input: in, output: out}
	now, err := temporal.NewDateTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	expires, _ := now.Add(time.Hour)
	body := []byte(`{"text":"private stored body"}`)
	return op, idempotencystore.Record{Fingerprint: "fingerprint", ResultSchema: out.identity, Representation: body, ResultHash: digest("foundry.idempotency.result.v1", string(body)), CompletedAt: value.Of(now), ExpiresAt: value.Of(expires)}
}
func TestPersistedOutcomeCorruptionAndSchemaMismatch(t *testing.T) {
	op, row := storedTestOutcome(t)
	if result, err := op.restore(t.Context(), row, "fingerprint"); err != nil || !result.Replayed() {
		t.Fatal(err)
	}
	if _, err := op.restore(t.Context(), row, "different"); !errors.Is(err, Mismatch) {
		t.Fatal("different payload was restored", err)
	}
	// A changed result contract replays when the current codec still accepts
	// the exact stored representation (compatible schema evolution).
	evolved := row
	evolved.ResultSchema = "previous-contract"
	if result, err := op.restore(t.Context(), evolved, "fingerprint"); err != nil || !result.Replayed() || result.Value().Text != "private stored body" || string(result.Encoded()) != string(row.Representation) {
		t.Fatal("compatible schema evolution did not replay the stored outcome", err)
	}
	for _, change := range []func(*idempotencystore.Record){func(r *idempotencystore.Record) {
		// An incompatible stored representation under a changed contract is
		// never re-executed or coerced; it remains unavailable.
		r.ResultSchema = "previous-contract"
		r.Representation = []byte(`{"text":7}`)
		r.ResultHash = digest("foundry.idempotency.result.v1", string(r.Representation))
	}, func(r *idempotencystore.Record) { r.ResultSchema = "" }, func(r *idempotencystore.Record) { r.ResultHash = "changed" }, func(r *idempotencystore.Record) { r.CompletedAt = value.Null[temporal.DateTime]() }, func(r *idempotencystore.Record) { r.ExpiresAt = r.CompletedAt }, func(r *idempotencystore.Record) { r.Representation = []byte(strings.Repeat("x", 5000)) }, func(r *idempotencystore.Record) {
		r.Representation = []byte(`{"text":`)
		r.ResultHash = digest("foundry.idempotency.result.v1", string(r.Representation))
	}} {
		changed := row
		change(&changed)
		if _, err := op.restore(t.Context(), changed, "fingerprint"); !errors.Is(err, Unavailable) {
			t.Fatal("invalid stored outcome replayed", err)
		}
	}
}
func FuzzStoredRepresentation(f *testing.F) {
	f.Add([]byte(`{"text":"valid"}`))
	f.Add([]byte(`{"text":null}`))
	f.Add([]byte(`{"text":`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		op, row := storedTestOutcome(t)
		row.Representation = data
		row.ResultHash = digest("foundry.idempotency.result.v1", string(data))
		result, err := op.restore(context.Background(), row, "fingerprint")
		if err == nil && !result.Replayed() {
			t.Fatal("accepted outcome was not marked replay")
		}
	})
}
