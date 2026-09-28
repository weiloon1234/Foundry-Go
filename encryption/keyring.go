package encryption

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Encrypt owns its output and generates its nonce in Go's standard AEAD. Context
// checks bound entry/exit; the bounded local cipher operation is not preemptible.
// An admitted encryption consumes budget even if cancellation discards output.
func (r *Keyring) Encrypt(ctx context.Context, binding Context, plaintext secret.String) (Ciphertext, error) {
	if err := r.validateOperation(ctx, binding); err != nil {
		return Ciphertext{}, err
	}
	if len(plaintext.Reveal()) > MaxPlaintextBytes {
		return Ciphertext{}, fault.New(fault.Invalid, "plaintext exceeds encryption limit")
	}
	if err := r.active.reserve(); err != nil {
		return Ciphertext{}, err
	}
	raw := r.active.aead.Seal(nil, nil, []byte(plaintext.Reveal()), binding.aad(r.active.id))
	if err := ctx.Err(); err != nil {
		return Ciphertext{}, err
	}
	encoded := version + ":" + string(r.active.id) + ":" + base64.RawURLEncoding.EncodeToString(raw)
	return Ciphertext{encoded: encoded, id: r.active.id}, nil
}

// Decrypt returns no plaintext on any failure. Missing keys and failed
// authentication share one error. It never accepts legacy or unauthenticated
// formats, retries other keys, or silently replaces a missing key.
func (r *Keyring) Decrypt(ctx context.Context, binding Context, ciphertext Ciphertext) (secret.String, error) {
	if err := r.validateOperation(ctx, binding); err != nil {
		return secret.String{}, err
	}
	if ciphertext.IsZero() {
		return secret.String{}, fault.New(fault.Invalid, "encryption envelope is missing")
	}
	failure := func() (secret.String, error) {
		return secret.String{}, fault.New(fault.Invalid, "encryption envelope could not be authenticated")
	}
	key := r.keys[ciphertext.id]
	if key == nil {
		return failure()
	}
	_, payload, ok := strings.Cut(ciphertext.encoded[len(version)+1:], ":")
	if !ok {
		return failure()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil {
		return failure()
	}
	plain, err := key.aead.Open(nil, nil, raw, binding.aad(key.id))
	if err != nil {
		return failure()
	}
	if err := ctx.Err(); err != nil {
		return secret.String{}, err
	}
	return secret.New(string(plain)), nil
}

// Reencrypt authenticates with the retained source key and creates a fresh
// envelope using the active key, preserving the exact context and plaintext.
// Callers own atomic record replacement; this method does no persistence.
func (r *Keyring) Reencrypt(ctx context.Context, binding Context, ciphertext Ciphertext) (Ciphertext, error) {
	plain, err := r.Decrypt(ctx, binding, ciphertext)
	if err != nil {
		return Ciphertext{}, err
	}
	return r.Encrypt(ctx, binding, plain)
}
func (r *Keyring) validateOperation(ctx context.Context, binding Context) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "encryption requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return binding.Validate()
}
