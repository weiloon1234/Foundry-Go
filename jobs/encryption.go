package jobs

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// payloadPurpose separates job payload ciphertext from other keyring uses.
const payloadPurpose encryption.Purpose = "foundry.jobs.payload.v1"

// Encrypted returns this definition with payload encryption: Capture seals the
// payload JSON with keyring's active key (authenticated to the job name and
// version) and workers decrypt it before decoding, so queues, outbox rows,
// archives and inspection store only ciphertext. Encrypted envelopes use
// ExtendedEnvelope. Producers and workers need the same keyring (rotation keeps
// old keys for decryption); a worker without it retries the job. Envelopes
// captured before encryption was enabled still decode as plaintext.
func (d Definition[P]) Encrypted(keyring *encryption.Keyring) Definition[P] {
	d.keyring = keyring
	return d
}

func (d Definition[P]) encryptionContext() (encryption.Context, error) {
	return encryption.NewContext(payloadPurpose, secret.New(string(d.name)+"\x00"+strconv.FormatUint(uint64(d.version), 10)))
}

// seal encrypts captured payload JSON into a JSON string of ciphertext.
func (d Definition[P]) seal(ctx context.Context, payload string) (string, error) {
	if err := d.keyring.Validate(); err != nil {
		return "", err
	}
	binding, err := d.encryptionContext()
	if err != nil {
		return "", err
	}
	ciphertext, err := d.keyring.Encrypt(ctx, binding, secret.New(payload))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(ciphertext.Encoded())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// plaintext returns an envelope's payload JSON, decrypting it when sealed.
func (d Definition[P]) plaintext(ctx context.Context, envelope Envelope) (string, error) {
	if !envelope.Encrypted() {
		return envelope.PayloadJSON(), nil
	}
	if d.keyring == nil {
		return "", fault.New(fault.Invalid, "encrypted job payload requires the definition's keyring")
	}
	var encoded string
	if err := json.Unmarshal([]byte(envelope.PayloadJSON()), &encoded); err != nil {
		return "", &payloadError{cause: fault.New(fault.Invalid, "encrypted job payload is malformed")}
	}
	ciphertext, err := encryption.ParseCiphertext(encoded)
	if err != nil {
		return "", &payloadError{cause: err}
	}
	binding, err := d.encryptionContext()
	if err != nil {
		return "", err
	}
	plaintext, err := d.keyring.Decrypt(ctx, binding, ciphertext)
	if err != nil {
		return "", err
	}
	return plaintext.Reveal(), nil
}

// Payload decodes (and decrypts) the payload of one of this definition's
// envelopes: an explicit private-data boundary for operator and test tooling.
func (d Definition[P]) Payload(ctx context.Context, envelope Envelope) (P, error) {
	var zero P
	if ctx == nil || envelope.Name() != d.name || envelope.Version() != d.version {
		return zero, fault.New(fault.Invalid, "envelope belongs to another job definition")
	}
	text, err := d.plaintext(ctx, envelope)
	if err != nil {
		return zero, err
	}
	snapshot, err := value.ParseJSON[P](text)
	if err != nil {
		return zero, err
	}
	return snapshot.Decode()
}
