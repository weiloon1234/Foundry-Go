package password

import "context"

// DummyCheck consumes the current issuance policy's verification work for a
// missing or unusable credential. It discards the comparison result and grants
// no authority. Login orchestration uses it to avoid a cheap missing-account
// branch. Different legacy costs and database latency still affect timing;
// this is not a claim of constant-time account discovery protection.
func (h *Hasher) DummyCheck(ctx context.Context, plain Plaintext) error {
	if err := h.Validate(); err != nil {
		return err
	}
	dummy := encodedHash(h.config.Parameters, make([]byte, SaltBytes), make([]byte, KeyBytes))
	_, err := h.Check(ctx, plain, dummy)
	return err
}
