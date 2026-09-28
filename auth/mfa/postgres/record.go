package postgres

import (
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/mfastore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func decode(address mfa.Address, row mfastore.Factor) (mfa.Record, error) {
	identity, err := row.Identity.Decode()
	if err != nil {
		return mfa.Record{}, err
	}
	scope, err := address.Key()
	if err != nil {
		return mfa.Record{}, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return mfa.Record{}, err
	}
	if row.Scope != scope || row.Key != key {
		return mfa.Record{}, fault.New(fault.Invalid, "stored MFA factor belongs to a different subject")
	}
	ciphertext, err := encryption.ParseCiphertext(row.Ciphertext)
	if err != nil {
		return mfa.Record{}, err
	}
	hashes, err := row.RecoveryHashes.Decode()
	if err != nil {
		return mfa.Record{}, err
	}
	if len(hashes) > mfa.MaxRecoveryCodes {
		return mfa.Record{}, fault.New(fault.Invalid, "stored MFA recovery capacity exceeded")
	}
	record := mfa.Record{ID: model.IDFromBytes[mfa.Record](row.Generation.Bytes()), Address: address, Subject: identity, Ciphertext: ciphertext, CreatedAt: row.CreatedAt, RecoveryHashes: make([]mfa.RecoveryHash, len(hashes))}
	for i, hash := range hashes {
		record.RecoveryHashes[i], err = mfa.ParseRecoveryHash(hash)
		if err != nil {
			return mfa.Record{}, err
		}
	}
	if v, ok := row.PendingUntil.Get(); ok {
		record.PendingUntil = value.Set(v)
	}
	if v, ok := row.ConfirmedAt.Get(); ok {
		record.ConfirmedAt = value.Set(v)
	}
	if v, ok := row.LastStep.Get(); ok {
		record.LastStep = value.Set(v)
	}
	return record, record.Validate(address, identity)
}
func encode(record mfa.Record) (mfastore.FactorDraft, error) {
	if err := record.Validate(record.Address, record.Subject); err != nil {
		return mfastore.FactorDraft{}, err
	}
	key, err := record.Address.SubjectKey(record.Subject)
	if err != nil {
		return mfastore.FactorDraft{}, err
	}
	scope, err := record.Address.Key()
	if err != nil {
		return mfastore.FactorDraft{}, err
	}
	identity, err := value.NewJSON(record.Subject)
	if err != nil {
		return mfastore.FactorDraft{}, err
	}
	hashes := make([]string, len(record.RecoveryHashes))
	for i, hash := range record.RecoveryHashes {
		hashes[i] = hash.Encoded()
	}
	stored, err := value.NewJSON(hashes)
	if err != nil {
		return mfastore.FactorDraft{}, err
	}
	draft := mfastore.FactorDraft{}.SetKey(key).SetScope(scope).SetIdentity(identity).SetGeneration(model.IDFromBytes[mfastore.Factor](record.ID.Bytes())).SetCiphertext(record.Ciphertext.Encoded()).SetCreatedAt(record.CreatedAt).ClearPendingUntil().ClearConfirmedAt().ClearLastStep().SetRecoveryHashes(stored)
	if v, ok := record.PendingUntil.Get(); ok {
		draft = draft.SetPendingUntil(v)
	}
	if v, ok := record.ConfirmedAt.Get(); ok {
		draft = draft.SetConfirmedAt(v)
	}
	if v, ok := record.LastStep.Get(); ok {
		draft = draft.SetLastStep(v)
	}
	return draft, nil
}
