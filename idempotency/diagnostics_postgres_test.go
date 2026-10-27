package idempotency

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestOperationDiagnosticsAcrossContainment(t *testing.T) {
	schema, db := testDatabase(t)
	store := testStore(t, db, schema.Schema(), func(c *Config) { c.MaxRetainedPerCaller = 1 })
	for _, test := range []struct {
		name    string
		code    Code
		message string
	}{
		{"bad-key", BadKey, "consumer: idempotency_bad_key"},
		{"capacity", Capacity, "consumer: idempotent operation: idempotency_capacity"},
		{"mismatch", Mismatch, "consumer: idempotent operation: idempotency_mismatch"},
		{"unavailable", Unavailable, "consumer: idempotent operation: idempotency_unavailable"},
		{"private-handler", "", "consumer failed"},
		{"application-code", Capacity, "consumer failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			op := testOperation(t, store, OperationID("diagnostic-"+test.name))
			scope := testScope(t, test.name)
			key := testKey(t, "diagnostic-key-0001")
			input := testInput{Text: "private input"}
			cause := &hostileDiagnosticCause{}
			handler := effect
			var identity error
			switch test.name {
			case "bad-key":
				key = Key{}
			case "capacity", "mismatch", "unavailable":
				seed, err := op.Run(t.Context(), scope, key, input, effect)
				if err != nil || !seed.Committed() {
					t.Fatal("could not seed committed outcome", err)
				}
				switch test.name {
				case "capacity":
					key = testKey(t, "diagnostic-key-0002")
				case "mismatch":
					input.Text = "different private input"
				case "unavailable":
					op.output.decode = func(context.Context, []byte, int) (testOutput, error) { return testOutput{}, cause }
					identity = cause
				}
			case "private-handler":
				identity = cause
				handler = func(context.Context, *database.Tx, testInput) (testOutput, error) { return testOutput{}, cause }
			case "application-code":
				identity = Capacity
				handler = func(context.Context, *database.Tx, testInput) (testOutput, error) { return testOutput{}, Capacity }
			}
			result, source := op.Run(t.Context(), scope, key, input, handler)
			got := callback.Isolated("consumer", func() error { return source })
			if got == nil || got.Error() != test.message || !errors.Is(got, source) {
				t.Fatal("operation diagnostic or identity was lost")
			}
			if test.code != "" && !errors.Is(got, test.code) {
				t.Fatal("operation classification was lost")
			}
			if identity != nil && !errors.Is(got, identity) {
				t.Fatal("application cause identity was lost")
			}
			if test.name != "private-handler" && test.name != "application-code" {
				var detail *Error
				if !errors.As(source, &detail) || !errors.Is(got, detail) {
					t.Fatal("typed operation identity was lost")
				}
			}
			if result.Committed() || result.Replayed() || cause.formats.Load() != 0 {
				t.Fatal("failed operation changed outcome or formatted application text")
			}
		})
	}
}
